package token

import (
	"github.com/aitra-ai/aitra-server/aigateway/types"
	"github.com/aitra-ai/aitra-server/builder/llm"
)

type sglangTokenizerImpl struct {
	endpoint string
	host     string
	model    string
	hc       llm.LLMSvcClient
}

func newSGLangTokenizerImpl(endpoint, host, model string) Tokenizer {
	return &sglangTokenizerImpl{
		endpoint: endpoint,
		host:     host,
		model:    model,
		hc:       llm.NewClient(),
	}
}

func (tk *sglangTokenizerImpl) Encode(message types.Message) (int64, error) {
	return 0, errUnsupportedTokenizer
}

func (tk *sglangTokenizerImpl) EmbeddingEncode(message string) (int64, error) {
	return 0, errUnsupportedTokenizer
}
