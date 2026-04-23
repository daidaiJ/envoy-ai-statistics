package usage

import (
	"context"
	"fmt"
	"strings"
	"tokenusage/internal/util"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

// 需要深度统计的 LLM API 路径（不含 query 参数）
var llmStatPaths = map[string]bool{
	"/v1/chat/completions": true,
	"/v1/messages":         true,
	"/v1/embeddings":       true,
}

// matchLLMPath 判断路径是否需要深度统计
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
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
	}

	reqHeaders := map[string]string{"model": "", ":path": "", "Authorization": ""}
	util.GetHeaders(headers, reqHeaders)

	reqCtx.Model = reqHeaders["model"]
	reqCtx.Path = reqHeaders[":path"]
	reqCtx.PathOnly, reqCtx.ShouldStat = matchLLMPath(reqCtx.Path)

	auth := strings.Split(reqHeaders["Authorization"], " ")
	if len(auth) > 1 {
		reqCtx.SK = auth[1]
	}

	if reqCtx.ShouldStat {
		fmt.Printf("[LLM统计] model: [%s], path: [%s], pathOnly: [%s], sk: [%s]\n",
			reqCtx.Model, reqCtx.Path, reqCtx.PathOnly, reqCtx.SK)
	} else {
		fmt.Printf("[跳过] path: [%s] (非LLM统计路径)\n", reqCtx.Path)
	}

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
}

// ProcessResponseHeaders 处理响应头
func (r *RouterProcessor) ProcessResponseHeaders(ctx context.Context, headers *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error) {
	reqCtx := getRequestCtx(ctx)
	if reqCtx == nil || !reqCtx.ShouldStat {
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseHeaders{}}, nil
	}

	for _, h := range headers.GetHeaders() {
		if h.GetKey() == "content-type" {
			contentType := h.GetValue()
			reqCtx.IsStreaming = strings.Contains(contentType, "text/event-stream")
			fmt.Printf("[LLM统计] 响应 Content-Type: %s (流式: %v)\n", contentType, reqCtx.IsStreaming)
			break
		}
	}

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

	reqCtx.bodyMu.Lock()
	reqCtx.bodyBuf = append(reqCtx.bodyBuf, body.Body...)
	reqCtx.bodyMu.Unlock()

	if body.EndOfStream {
		reqCtx.printRecordedBody()
		reqCtx.parseUsageFromSSE()
	}

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{}}, nil
}