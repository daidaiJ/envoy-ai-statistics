package resp

import "fmt"

type PromptTokensDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

type Usage struct {
	CompletionTokens    int64               `json:"completion_tokens"`
	PromptTokens        int64               `json:"prompt_tokens"`
	PromptTokensDetails PromptTokensDetails `json:"prompt_tokens_details"`
	TotalTokens         int64               `json:"total_tokens"`
}

type OpenAIResp struct {
	Model string `json:"model"`
	Usage Usage  `json:"usage"`
}

func (o *OpenAIResp) GetModel() string {
	return o.Model
}

func (o *OpenAIResp) GetInputToken() int64 {
	return o.Usage.PromptTokens
}

func (o *OpenAIResp) GetOutputToken() int64 {
	return o.Usage.CompletionTokens
}

func (o *OpenAIResp) GetCachedToken() int64 {
	return o.Usage.PromptTokensDetails.CachedTokens
}

func (o *OpenAIResp) String() string {
	return fmt.Sprintf("Model: %s | Input: %d | Output: %d | Cached: %d | Total: %d",
		o.Model,
		o.Usage.PromptTokens,
		o.Usage.CompletionTokens,
		o.Usage.PromptTokensDetails.CachedTokens,
		o.Usage.TotalTokens,
	)
}
