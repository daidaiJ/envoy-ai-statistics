package resp

import (
	"strings"
	"sync"
)

type Responser interface {
	GetModel() string
	GetCachedToken() int64
	GetInputToken() int64
	GetOutputToken() int64
	String() string
}

// 对象池
var (
	openaiPool = sync.Pool{
		New: func() any { return &OpenAIResp{} },
	}
	anthropicPool = sync.Pool{
		New: func() any { return &AnthropicResp{} },
	}
)

// GetResponser 根据路径获取对应的 Responser（从对象池）
// OpenAI: /v1/chat/completions, /v1/completions, /v1/embeddings
// Anthropic: /v1/messages, /v1/complete
func GetResponser(path string) Responser {
	// 提取纯路径（去掉 query）
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}

	switch {
	case strings.HasSuffix(path, "/messages"),
		strings.HasSuffix(path, "/complete"):
		return anthropicPool.Get().(*AnthropicResp)
	default:
		// OpenAI 及未知类型默认使用 OpenAI 解析
		return openaiPool.Get().(*OpenAIResp)
	}
}

// PutResponser 放回对象池
func PutResponser(r Responser) {
	if r == nil {
		return
	}
	switch v := r.(type) {
	case *OpenAIResp:
		v.Model = ""
		v.Usage = Usage{}
		openaiPool.Put(v)
	case *AnthropicResp:
		v.Model = ""
		v.Usage = anthropicUsage{}
		anthropicPool.Put(v)
	}
}
