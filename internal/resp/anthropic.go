package resp

import "fmt"

type anthropicUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type AnthropicResp struct {
	Model string         `json:"model"`
	Usage anthropicUsage `json:"usage"`
}

func (a *AnthropicResp) GetInputToken() int64 {
	return a.Usage.InputTokens
}

func (a *AnthropicResp) GetOutputToken() int64 {
	return a.Usage.OutputTokens
}

func (a *AnthropicResp) GetCachedToken() int64 {
	return 0
}

func (a *AnthropicResp) GetModel() string {
	return a.Model
}

func (a *AnthropicResp) String() string {
	return fmt.Sprintf("Model: %s | Input: %d | Output: %d | Cached: %d | Total: %d",
		a.Model,
		a.Usage.InputTokens,
		a.Usage.OutputTokens,
		0,
		a.Usage.InputTokens+a.Usage.OutputTokens,
	)
}
