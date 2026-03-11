package handler

import (
	"encoding/json"
	"fmt"
	"time"
)

// anthropicToOpenAIResponse converts a non-streaming Anthropic Messages API response
// into an OpenAI Chat Completions response format.
// Returns nil if the data is not a valid Anthropic response.
func anthropicToOpenAIResponse(data []byte) ([]byte, bool) {
	var ar struct {
		ID   string `json:"id"`
		Type string `json:"type"` // "message"
		Role string `json:"role"` // "assistant"
		Content []struct {
			Type string `json:"type"` // "text"
			Text string `json:"text"`
		} `json:"content"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, false
	}
	// Must have type=message and at least one content block
	if ar.Type != "message" || len(ar.Content) == 0 {
		return nil, false
	}

	// Concatenate all text blocks
	text := ""
	for _, c := range ar.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}

	finishReason := "stop"
	switch ar.StopReason {
	case "max_tokens":
		finishReason = "length"
	case "tool_use":
		finishReason = "tool_calls"
	}

	oai := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%s", ar.ID),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   ar.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     ar.Usage.InputTokens,
			"completion_tokens": ar.Usage.OutputTokens,
			"total_tokens":      ar.Usage.InputTokens + ar.Usage.OutputTokens,
		},
	}
	out, err := json.Marshal(oai)
	if err != nil {
		return nil, false
	}
	return out, true
}

// anthropicSSEToOpenAISSE converts a single Anthropic SSE data line to zero or more
// OpenAI-format SSE data lines. Returns empty string for events that should be suppressed.
// The last bool return is true when the stream is finished (message_stop).
func anthropicSSEToOpenAISSE(line string, msgID *string, model *string) (output string, done bool) {
	// Anthropic SSE: "data: <json>"
	const prefix = "data: "
	if len(line) < len(prefix) {
		return "", false
	}
	raw := line[len(prefix):]

	var evt map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		return "", false
	}

	evtType := ""
	if t, ok := evt["type"]; ok {
		_ = json.Unmarshal(t, &evtType)
	}

	switch evtType {
	case "message_start":
		// Extract id and model from the nested message object
		var ms struct {
			Message struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &ms); err == nil {
			*msgID = ms.Message.ID
			*model = ms.Message.Model
		}
		return "", false

	case "content_block_delta":
		var cbd struct {
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Index int `json:"index"`
		}
		if err := json.Unmarshal([]byte(raw), &cbd); err != nil || cbd.Delta.Type != "text_delta" {
			return "", false
		}

		id := *msgID
		if id == "" {
			id = "msg-unknown"
		}
		mod := *model

		chunk := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-%s", id),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   mod,
			"choices": []map[string]interface{}{
				{
					"index": cbd.Index,
					"delta": map[string]interface{}{
						"content": cbd.Delta.Text,
					},
					"finish_reason": nil,
				},
			},
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			return "", false
		}
		return "data: " + string(b) + "\n\n", false

	case "message_delta":
		// Contains stop_reason and output usage; emit final chunk
		var md struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(raw), &md); err != nil {
			return "", false
		}
		finishReason := "stop"
		if md.Delta.StopReason == "max_tokens" {
			finishReason = "length"
		}
		id := *msgID
		if id == "" {
			id = "msg-unknown"
		}
		chunk := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-%s", id),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   *model,
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         map[string]interface{}{},
					"finish_reason": finishReason,
				},
			},
		}
		b, _ := json.Marshal(chunk)
		return "data: " + string(b) + "\n\n", false

	case "message_stop":
		return "data: [DONE]\n\n", true

	default:
		// ping, content_block_start, content_block_stop, error — suppress
		return "", false
	}
}
