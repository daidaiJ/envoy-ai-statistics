//go:build gjson

package resp

import "github.com/tidwall/gjson"

// extract 使用 gjson 按路径直接提取 JSON 字段，避免完整结构体反序列化。
//
// 优化点：
//   - 零结构体分配：不创建 OpenAIResp/AnthropicResp，直接提取标量字段
//   - 按需读取：只访问 usage 相关路径，跳过 choices/messages 等大数组
//   - 纯 Go 实现，无 CGO 依赖，适合交叉编译
//
// OpenAI 路径: usage.prompt_tokens, usage.completion_tokens, usage.prompt_tokens_details.cached_tokens
// Anthropic 路径: usage.input_tokens, usage.output_tokens
func extract(data []byte) *UsageRaw {
	if !gjson.GetBytes(data, "usage").Exists() {
		return nil
	}

	model := gjson.GetBytes(data, "model").String()

	// OpenAI 格式
	inputToken := gjson.GetBytes(data, "usage.prompt_tokens").Int()
	if inputToken > 0 {
		return &UsageRaw{
			Model:       model,
			InputToken:  inputToken,
			OutputToken: gjson.GetBytes(data, "usage.completion_tokens").Int(),
			CachedToken: gjson.GetBytes(data, "usage.prompt_tokens_details.cached_tokens").Int(),
		}
	}

	// Anthropic 格式
	inputToken = gjson.GetBytes(data, "usage.input_tokens").Int()
	if inputToken > 0 {
		return &UsageRaw{
			Model:       model,
			InputToken:  inputToken,
			OutputToken: gjson.GetBytes(data, "usage.output_tokens").Int(),
		}
	}

	return emptyUsageResult
}
