package usage

import (
	"context"
	"strings"
	"tokenusage/internal/util"
	"tokenusage/pkg/logger"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

// 需要统计的 LLM API 路径（主流文本生成/嵌入服务）
// OpenAI/兼容格式: /v1/chat/completions, /v1/completions, /v1/embeddings
// Anthropic格式: /v1/messages
var llmStatPaths = map[string]bool{
	"/v1/chat/completions": true, // ChatGPT风格对话（主流）
	"/v1/completions":      true, // 旧版补全（部分服务仍用）
	"/v1/messages":         true, // Anthropic风格
	"/v1/embeddings":       true, // 向量嵌入
}

var reqHeaders = map[string]string{":path": "", ":method": "", "authorization": "", "maas-inference-service": ""}

// matchLLMPath 判断路径是否需要统计
func matchLLMPath(path string) (pathOnly string, shouldStat bool) {
	// 去掉 query 参数
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		pathOnly = path[:idx]
	} else {
		pathOnly = path
	}
	shouldStat = llmStatPaths[pathOnly]
	return pathOnly, shouldStat
}

// Processor 定义 ext_proc 处理接口
type Processor interface {
	ProcessRequestHeaders(context.Context, *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error)
	ProcessRequestBody(context.Context, *extprocv3.HttpBody) (*extprocv3.ProcessingResponse, error)
	ProcessResponseHeaders(context.Context, *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error)
	ProcessResponseBody(context.Context, *extprocv3.HttpBody) (*extprocv3.ProcessingResponse, error)
}

// passThroughProcessor 默认透传处理器
type passThroughProcessor struct{}

func (p passThroughProcessor) ProcessRequestHeaders(context.Context, *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error) {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
}

func (p passThroughProcessor) ProcessRequestBody(context.Context, *extprocv3.HttpBody) (*extprocv3.ProcessingResponse, error) {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestBody{}}, nil
}

func (p passThroughProcessor) ProcessResponseHeaders(context.Context, *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error) {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseHeaders{}}, nil
}

func (p passThroughProcessor) ProcessResponseBody(context.Context, *extprocv3.HttpBody) (*extprocv3.ProcessingResponse, error) {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{}}, nil
}

// RouterProcessor LLM 统计处理器
type RouterProcessor struct {
	passThroughProcessor
}

// ProcessRequestHeaders 处理请求头
func (r *RouterProcessor) ProcessRequestHeaders(ctx context.Context, headers *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error) {
	reqCtx := getRequestCtx(ctx)
	if reqCtx == nil {
		logger.Warn("request context is nil")
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
	}

	util.GetHeaders(headers, reqHeaders)

	// 第一步：只处理 POST 请求，其他方法直接跳过
	method := reqHeaders[":method"]
	if method != "POST" {
		reqCtx.ShouldStat = false
		logger.Debug("跳过非POST请求", "method", method, "path", reqHeaders[":path"])
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
	}

	// 第二步：判断路径是否为LLM统计路径
	reqCtx.Path = reqHeaders[":path"]
	reqCtx.PathOnly, reqCtx.ShouldStat = matchLLMPath(reqCtx.Path)
	if !reqCtx.ShouldStat {
		logger.Debug("跳过非LLM统计路径", "path", reqCtx.Path)
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
	}

	auth := strings.Split(reqHeaders["authorization"], " ")
	if len(auth) > 1 {
		reqCtx.SK = auth[1]
	}
	reqCtx.InferenceId = reqHeaders["maas-inference-service"]
	logger.Debug("LLM请求统计", "path", reqCtx.Path, "sk", reqCtx.SK, "maas-inference-service", reqCtx.InferenceId)

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
}

// ProcessResponseHeaders 处理响应头
func (r *RouterProcessor) ProcessResponseHeaders(ctx context.Context, headers *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error) {

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseHeaders{}}, nil
}

// ProcessResponseBody 处理响应体
func (r *RouterProcessor) ProcessResponseBody(ctx context.Context, body *extprocv3.HttpBody) (*extprocv3.ProcessingResponse, error) {
	reqCtx := getRequestCtx(ctx)
	if reqCtx == nil || !reqCtx.ShouldStat {
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{}}, nil
	}

	if len(body.Body) > 0 {
		reqCtx.recordBodyChunk(body.Body)
	}

	if body.EndOfStream {
		reqCtx.printRecordedBody()
	}

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{}}, nil
}
