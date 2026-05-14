package usage

import (
	"bytes"
	"context"
	"strings"
	"time"

	"tokenusage/config"
	"tokenusage/internal/util"
	"tokenusage/pkg/logger"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

// 需要统计的 LLM API 路径（主流文本生成/嵌入服务）
var llmStatPaths = map[string]bool{
	"/v1/chat/completions": true,
	"/v1/completions":      true,
	"/v1/messages":         true,
	"/v1/embeddings":       true,
}

// 需要提取的请求头 key（小写）
var headerKeys = []string{":path", ":method", "authorization", "maas-inference-service"}

// matchLLMPath 判断路径是否需要统计
func matchLLMPath(path string) (pathOnly string, shouldStat bool) {
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		pathOnly = path[:idx]
	} else {
		pathOnly = path
	}
	shouldStat = llmStatPaths[pathOnly]
	return pathOnly, shouldStat
}

// maskSK 按配置掩码 SK：保留末尾 n 个字符，其余替换为 "***"
func maskSK(sk string, maskLen int) string {
	if sk == "" {
		return ""
	}
	if maskLen <= 0 || len(sk) <= maskLen {
		return "***"
	}
	return "***" + sk[len(sk)-maskLen:]
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
	cfg *config.Config
}

// NewRouterProcessor 创建 RouterProcessor
func NewRouterProcessor(cfg *config.Config) *RouterProcessor {
	return &RouterProcessor{cfg: cfg}
}

// ProcessRequestHeaders 处理请求头
func (r *RouterProcessor) ProcessRequestHeaders(ctx context.Context, headers *corev3.HeaderMap) (*extprocv3.ProcessingResponse, error) {
	reqCtx := getRequestCtx(ctx)
	if reqCtx == nil {
		logger.Warn("request context is nil")
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
	}

	// 使用局部 map，避免并发竞态
	local := make(map[string]string, len(headerKeys))
	for _, k := range headerKeys {
		local[k] = ""
	}
	util.GetHeaders(headers, local)

	method := local[":method"]
	if method != "POST" {
		reqCtx.ShouldStat = false
		logger.Debug("跳过非POST请求", "method", method, "path", local[":path"])
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
	}

	reqCtx.Path = local[":path"]
	reqCtx.PathOnly, reqCtx.ShouldStat = matchLLMPath(reqCtx.Path)
	if !reqCtx.ShouldStat {
		logger.Debug("跳过非LLM统计路径", "path", reqCtx.Path)
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
	}

	reqCtx.StartTime = time.Now()

	auth := strings.SplitN(local["authorization"], " ", 2)
	if len(auth) > 1 {
		reqCtx.SK = auth[1]
	}
	reqCtx.InferenceId = local["maas-inference-service"]
	logger.Info("LLM统计请求", "path", reqCtx.Path, "sk", maskSK(reqCtx.SK, r.cfg.MaskLen), "inference_service", reqCtx.InferenceId)
	if reqCtx.SK == "" {
		reqCtx.ShouldStat = false
	}

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{}}, nil
}

// ProcessRequestBody 处理请求体 - 拦截并修改流式请求以强制返回 usage
func (r *RouterProcessor) ProcessRequestBody(ctx context.Context, body *extprocv3.HttpBody) (*extprocv3.ProcessingResponse, error) {
	reqCtx := getRequestCtx(ctx)
	if reqCtx == nil || !reqCtx.ShouldStat {
		return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestBody{}}, nil
	}

	if reqCtx.Count == 0 && len(body.Body) > 0 {
		logger.Debug("处理第一个 RequestBody chunk", "size", len(body.Body))
		modifiedBody := r.enforceUsageInRequest(body.Body, reqCtx.PathOnly)
		if modifiedBody != nil {
			logger.Debug("成功修改请求体", "original_size", len(body.Body), "modified_size", len(modifiedBody))
			return &extprocv3.ProcessingResponse{
				Response: &extprocv3.ProcessingResponse_RequestBody{
					RequestBody: &extprocv3.BodyResponse{
						Response: &extprocv3.CommonResponse{
							BodyMutation: &extprocv3.BodyMutation{
								Mutation: &extprocv3.BodyMutation_Body{
									Body: modifiedBody,
								},
							},
						},
					},
				},
			}, nil
		}
		logger.Debug("未修改请求体")
	}

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestBody{}}, nil
}

// EnforceUsageInRequest 公开版本用于测试
func (r *RouterProcessor) EnforceUsageInRequest(body []byte) []byte {
	return r.enforceUsageInRequest(body, "")
}

// shouldInjectStreamOptions 根据配置判断是否对该路径注入 stream_options
func (r *RouterProcessor) shouldInjectStreamOptions(pathOnly string) bool {
	if r.cfg.StreamOptions.Disabled {
		return false
	}
	for _, p := range r.cfg.StreamOptions.Paths {
		if p == pathOnly {
			return true
		}
	}
	return false
}

// enforceUsageInRequest 修改请求体,添加强制返回 usage 的字段
func (r *RouterProcessor) enforceUsageInRequest(body []byte, pathOnly string) []byte {
	if pathOnly != "" && !r.shouldInjectStreamOptions(pathOnly) {
		logger.Debug("路径不在 stream_options 注入列表中", "path", pathOnly)
		return nil
	}

	streamIdx := bytes.Index(body, []byte(`"stream"`))
	if streamIdx < 0 {
		return nil
	}

	rest := body[streamIdx:]
	hasStreamTrue := bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream":true`)) ||
		bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream": true`)) ||
		bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream": true`)) ||
		bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream":true`))

	if !hasStreamTrue {
		return nil
	}

	if bytes.Contains(body, []byte(`"include_usage"`)) {
		return nil
	}

	if bytes.Contains(body, []byte(`"stream_options"`)) {
		return injectIntoStreamOptions(body)
	}

	return appendStreamOptions(body)
}

// injectIntoStreamOptions 在已有的 stream_options 对象中注入 include_usage
func injectIntoStreamOptions(body []byte) []byte {
	idx := bytes.Index(body, []byte(`"stream_options"`))
	if idx < 0 {
		return body
	}

	rest := body[idx:]
	openBrace := bytes.IndexByte(rest, '{')
	if openBrace < 0 {
		return body
	}

	absPos := idx + openBrace + 1

	closeBrace := bytes.IndexByte(rest[openBrace:], '}')
	if closeBrace < 0 {
		return body
	}

	if closeBrace == 1 {
		result := make([]byte, 0, len(body)+30)
		result = append(result, body[:absPos]...)
		result = append(result, []byte(`"include_usage":true}`)...)
		result = append(result, body[absPos+1:]...)
		return result
	}

	absCloseBrace := idx + openBrace + closeBrace
	result := make([]byte, 0, len(body)+35)
	result = append(result, body[:absCloseBrace]...)
	result = append(result, []byte(`,"include_usage":true}`)...)
	result = append(result, body[absCloseBrace+1:]...)
	return result
}

// appendStreamOptions 在 JSON 末尾追加 stream_options 字段
func appendStreamOptions(body []byte) []byte {
	trimmed := bytes.TrimRight(body, " \t\n\r")
	if len(trimmed) == 0 || trimmed[len(trimmed)-1] != '}' {
		return body
	}

	result := make([]byte, 0, len(body)+40)
	result = append(result, trimmed[:len(trimmed)-1]...)
	result = append(result, []byte(`,"stream_options":{"include_usage":true}}`)...)
	return result
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
		if reqCtx.Count == 0 {
			reqCtx.FirstChunkTime = time.Now()
		}
		reqCtx.recordBodyChunk(body.Body)
	}

	if body.EndOfStream {
		reqCtx.printRecordedBody()
	}

	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{}}, nil
}
