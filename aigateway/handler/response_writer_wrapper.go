package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/aitra-ai/aitra-server/aigateway/component"
	"github.com/aitra-ai/aitra-server/aigateway/token"
	"github.com/aitra-ai/aitra-server/aigateway/types"
)

type CommonResponseWriter interface {
	Header() http.Header
	WriteHeader(int)
	Write([]byte) (int, error)
	Flush()
	ClearBuffer()
	// GetCapturedTokens returns (inputTokens, outputTokens) captured from the response.
	// Works for both OpenAI and Anthropic response formats.
	GetCapturedTokens() (int, int)
}

var ErrSensitiveContent = errors.New("sensitive content detected")

var _ http.Hijacker = (*ResponseWriterWrapper)(nil)

type ResponseWriterWrapper struct {
	internalWritter     gin.ResponseWriter
	moderationComponent component.Moderation
	eventStreamDecoder  *eventStreamDecoder
	tokenCounter        token.ChatTokenCounter
	id                  string
	// Raw token counts captured directly from response (supports Anthropic + OpenAI formats)
	capturedInput  int
	capturedOutput int
	// convertAnthropic: when true, translate Anthropic SSE events → OpenAI SSE chunks
	convertAnthropic bool
	// state for Anthropic SSE conversion
	anthropicMsgID string
	anthropicModel string
}

func (rw *ResponseWriterWrapper) GetCapturedTokens() (int, int) {
	return rw.capturedInput, rw.capturedOutput
}

// Hijack allows the HTTP connection upgrading to a different protocol, such as WebSockets or HTTP/2.
func (rw *ResponseWriterWrapper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.internalWritter.Hijack()
}

func NewResponseWriterWrapper(internalWritter gin.ResponseWriter, useStream bool, moderationComponent component.Moderation, tokenCounter token.ChatTokenCounter) CommonResponseWriter {
	return NewResponseWriterWrapperWithOptions(internalWritter, useStream, moderationComponent, tokenCounter, false)
}

func NewResponseWriterWrapperWithOptions(internalWritter gin.ResponseWriter, useStream bool, moderationComponent component.Moderation, tokenCounter token.ChatTokenCounter, convertAnthropic bool) CommonResponseWriter {
	if useStream {
		return newStreamResponseWriter(internalWritter, moderationComponent, tokenCounter, convertAnthropic)
	} else {
		return newNonStreamResponseWriter(internalWritter, moderationComponent, tokenCounter, convertAnthropic)
	}
}

func newStreamResponseWriter(internalWritter gin.ResponseWriter, moderationComponent component.Moderation, tokenCounter token.ChatTokenCounter, convertAnthropic bool) *ResponseWriterWrapper {
	id := uuid.New().ID()
	return &ResponseWriterWrapper{
		internalWritter:     internalWritter,
		moderationComponent: moderationComponent,
		tokenCounter:        tokenCounter,
		eventStreamDecoder:  &eventStreamDecoder{},
		id:                  fmt.Sprint(id),
		convertAnthropic:    convertAnthropic,
	}
}

func (rw *ResponseWriterWrapper) ClearBuffer() {}

func (rw *ResponseWriterWrapper) Header() http.Header {
	return rw.internalWritter.Header()
}

func (rw *ResponseWriterWrapper) WriteHeader(statusCode int) {
	rw.internalWritter.WriteHeader(statusCode)
}

func (rw *ResponseWriterWrapper) Write(data []byte) (int, error) {
	return rw.streamWrite(data)
}

func (rw *ResponseWriterWrapper) streamWrite(data []byte) (int, error) {
	// Fast path: Anthropic SSE conversion (line-by-line, no OpenAI chunk parsing)
	if rw.convertAnthropic {
		lines := splitSSELines(data)
		for _, line := range lines {
			if len(line) == 0 {
				continue
			}
			out, done := anthropicSSEToOpenAISSE(line, &rw.anthropicMsgID, &rw.anthropicModel)
			if out != "" {
				rw.writeInternal([]byte(out))
			}
			if done {
				return len(data), nil
			}
		}
		return len(data), nil
	}

	events, _ := rw.eventStreamDecoder.Write(data)
	// unmarshal event data into ChatCompletionChunk and call moderation service
	for _, event := range events {
		if len(event.Data) <= 0 {
			continue
		}
		if string(event.Data) == "[DONE]" {
			rw.writeInternal(event.Raw)
			return len(data), nil
		}
		// unmarshal event data into ChatCompletionChunk
		var chunk types.ChatCompletionChunk
		err := json.Unmarshal(event.Data, &chunk)
		if err != nil {
			slog.Error("ResponseWriterWrapper streamWrite unmarshal error", slog.Any("err", err))
			rw.writeInternal(event.Raw)
			continue
		}
		if rw.tokenCounter != nil {
			rw.tokenCounter.AppendCompletionChunk(chunk)
		}
		// Capture token counts — support both OpenAI and Anthropic streaming formats.
		// OpenAI: final chunk has chunk.Usage.PromptTokens / CompletionTokens
		if chunk.Usage.PromptTokens > 0 {
			rw.capturedInput = int(chunk.Usage.PromptTokens)
		}
		if chunk.Usage.CompletionTokens > 0 {
			rw.capturedOutput = int(chunk.Usage.CompletionTokens)
		}
		// Anthropic: "message_start" event → usage.input_tokens; "message_delta" → usage.output_tokens
		var anthropicEvt struct {
			Type  string `json:"type"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if jerr := json.Unmarshal(event.Data, &anthropicEvt); jerr == nil {
			switch anthropicEvt.Type {
			case "message_start":
				if anthropicEvt.Message.Usage.InputTokens > 0 {
					rw.capturedInput = anthropicEvt.Message.Usage.InputTokens
				}
			case "message_delta":
				if anthropicEvt.Usage.OutputTokens > 0 {
					rw.capturedOutput = anthropicEvt.Usage.OutputTokens
				}
			}
		}
		// call moderation service
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := rw.moderationComponent.CheckChatStreamResponse(ctx, chunk, rw.id)
		if err != nil {
			slog.Error("ResponseWriterWrapper streamWrite checkChatResponse error", slog.Any("err", err))
			rw.writeInternal(event.Raw)
			continue
		}
		if result.IsSensitive {
			slog.Debug("ResponseWriterWrapper streamWrite checkresult is sensitive",
				slog.Any("content", chunk),
				slog.Any("reason", result.Reason))
			chunk = rw.generateSensitiveRespForContent(chunk)
			chunkJson, _ := json.Marshal(chunk)
			rw.writeInternal([]byte("data: " + string(chunkJson) + "\n\n"))
			rw.writeInternal([]byte("data: [DONE]\n\n"))
			return 0, ErrSensitiveContent
		}
		rw.writeInternal(event.Raw)
	}

	return len(data), nil
}

func (rw *ResponseWriterWrapper) writeInternal(data []byte) {
	slog.Debug("writeInternal", slog.String("data", string(data)))
	_, err := rw.internalWritter.Write(data)
	if err != nil {
		slog.Error("write into internalWritter error:", slog.String("err", err.Error()))
	}
	rw.internalWritter.Flush()
}

// TODO: support different Chunk struct
// func (rw *ResponseWriterWrapper) getData(value []byte) (openai.ChatCompletionChunk, error) {
// 	var cur openai.ChatCompletionChunk
// 	ep := gjson.GetBytes(value, "error")
// 	if ep.Exists() {
// 		return openai.ChatCompletionChunk{}, fmt.Errorf("error while streaming: %v", ep.String())
// 	}
// 	err := json.Unmarshal(value, &cur)
// 	if err != nil {
// 		return openai.ChatCompletionChunk{}, err
// 	}
// 	return cur, nil
// }

func (rw *ResponseWriterWrapper) generateSensitiveRespForContent(curChunk types.ChatCompletionChunk) types.ChatCompletionChunk {
	newChunk := types.ChatCompletionChunk{
		ID:    curChunk.ID,
		Model: curChunk.Model,
		Choices: []types.ChatCompletionChunkChoice{
			{
				Delta: types.ChatCompletionChunkChoiceDelta{
					Content: "The message includes inappropriate content and has been blocked. We appreciate your understanding and cooperation.",
				},
				FinishReason: "sensitive",
				Index:        curChunk.Choices[0].Index,
			},
		},
		SystemFingerprint: curChunk.SystemFingerprint,
		Object:            curChunk.Object,
		Usage:             curChunk.Usage,
	}
	return newChunk
}

func generateSensitiveRespForPrompt() types.ChatCompletionChunk {
	newChunk := types.ChatCompletionChunk{
		Choices: []types.ChatCompletionChunkChoice{
			{
				Delta: types.ChatCompletionChunkChoiceDelta{
					Content: "The prompt includes inappropriate content and has been blocked. We appreciate your understanding and cooperation.",
				},
				FinishReason: "sensitive",
				Index:        0,
			},
		},
	}
	return newChunk
}

func generateInsufficientBalanceResp(frontendURL string) types.ChatCompletionChunk {
	rechargeURL := fmt.Sprintf("%s/settings/recharge-payment", frontendURL)
	message := fmt.Sprintf(
		"**Insufficient balance**\n\n👉 [Recharge your account](%s) to continue.",
		rechargeURL,
	)
	newChunk := types.ChatCompletionChunk{
		Choices: []types.ChatCompletionChunkChoice{
			{
				Delta: types.ChatCompletionChunkChoiceDelta{
					Content: message,
				},
				FinishReason: "insufficient_balance",
				Index:        0,
			},
		},
	}
	return newChunk
}

func (rw *ResponseWriterWrapper) Flush() {
	rw.internalWritter.Flush()
}

// splitSSELines splits raw SSE bytes into individual "data: ..." lines.
func splitSSELines(data []byte) []string {
	var lines []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			line := string(data[start:i])
			// trim \r
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
