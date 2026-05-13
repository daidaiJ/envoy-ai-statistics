//go:build !gjson && !sonicjson

package resp

import (
	"sync"

	jsoniter "github.com/json-iterator/go"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

var (
	openaiPool = sync.Pool{New: func() any { return new(OpenAIResp) }}
	anthPool   = sync.Pool{New: func() any { return new(AnthropicResp) }}
)

// extract 使用 json-iterator 将 JSON 字节反序列化为结构体，再提取 usage 字段。
// 结构体通过 sync.Pool 复用，避免每次调用堆分配。
func extract(data []byte) *UsageRaw {
	openai := openaiPool.Get().(*OpenAIResp)
	if err := json.Unmarshal(data, openai); err != nil {
		openaiPool.Put(openai)
		return nil
	}
	if openai.Usage.PromptTokens > 0 {
		r := &UsageRaw{
			Model:       openai.Model,
			InputToken:  openai.Usage.PromptTokens,
			OutputToken: openai.Usage.CompletionTokens,
			CachedToken: openai.Usage.PromptTokensDetails.CachedTokens,
		}
		openaiPool.Put(openai)
		return r
	}
	openaiPool.Put(openai)

	anth := anthPool.Get().(*AnthropicResp)
	if err := json.Unmarshal(data, anth); err != nil {
		anthPool.Put(anth)
		return nil
	}
	if anth.Usage.InputTokens > 0 {
		r := &UsageRaw{
			Model:       anth.Model,
			InputToken:  anth.Usage.InputTokens,
			OutputToken: anth.Usage.OutputTokens,
		}
		anthPool.Put(anth)
		return r
	}
	anthPool.Put(anth)

	return emptyUsageResult
}
