package usage

import (
	"bytes"
	"context"
	"strings"
	"time"
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

	reqCtx.StartTime = time.Now()

	auth := strings.Split(reqHeaders["authorization"], " ")
	if len(auth) > 1 {
		reqCtx.SK = auth[1]
	}
	reqCtx.InferenceId = reqHeaders["maas-inference-service"]
	logger.Info("LLM统计请求", "path", reqCtx.Path, "sk", reqCtx.SK, "inference_service", reqCtx.InferenceId)
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

	// 只在第一个 body chunk 时处理(通常是完整请求体)
	if reqCtx.Count == 0 && len(body.Body) > 0 {
		logger.Debug("处理第一个 RequestBody chunk", "size", len(body.Body))
		modifiedBody := r.enforceUsageInRequest(body.Body)
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
	return r.enforceUsageInRequest(body)
}

// enforceUsageInRequest 修改请求体,添加强制返回 usage 的字段
// 针对 vLLM/SGLang 等 OpenAI 兼容服务:
// - 流式请求: 添加 stream_options.include_usage = true
// - 非流式请求: vLLM/SGLang 默认返回 usage,无需修改
//
// 使用字节流直接修改,避免对长上下文请求做全量 JSON 反序列化/序列化
func (r *RouterProcessor) enforceUsageInRequest(body []byte) []byte {
	logger.Debug("原始请求体前200字节", "preview", string(body[:min(len(body), 200)]))

	// 快速检查是否为流式请求
	streamIdx := bytes.Index(body, []byte(`"stream"`))
	if streamIdx < 0 {
		logger.Debug("未找到 stream 字段,非流式请求,无需修改")
		return nil // 无 stream 字段,非流式请求,无需修改
	}

	// 检查 "stream": true (跳过空白和引号)
	rest := body[streamIdx:]
	logger.Debug("stream 字段位置", "preview", string(rest[:min(len(rest), 30)]))

	hasStreamTrue := bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream":true`)) ||
		bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream": true`)) ||
		bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream": true`)) ||
		bytes.Contains(rest[:min(len(rest), 30)], []byte(`"stream":true`))

	if !hasStreamTrue {
		logger.Debug("stream 不为 true,非流式请求")
		return nil // stream 不为 true,非流式请求
	}

	logger.Debug("检测到流式请求,检查是否已有 stream_options")
	// 是流式请求,检查是否已有 stream_options.include_usage
	if bytes.Contains(body, []byte(`"include_usage"`)) {
		logger.Debug("已存在 include_usage,无需修改")
		return nil // 已存在,无需修改
	}

	// 检查是否已有 stream_options
	if bytes.Contains(body, []byte(`"stream_options"`)) {
		logger.Debug("存在 stream_options 但无 include_usage,尝试注入")
		// 有 stream_options 但无 include_usage,在 stream_options 的 } 前注入
		result := injectIntoStreamOptions(body)
		logger.Debug("注入后请求体前200字节", "preview", string(result[:min(len(result), 200)]))
		return result
	}

	logger.Debug("无 stream_options,追加到末尾")
	// 无 stream_options,在 JSON 末尾追加
	result := appendStreamOptions(body)
	logger.Debug("追加后请求体前200字节", "preview", string(result[:min(len(result), 200)]))
	return result
}

// injectIntoStreamOptions 在已有的 stream_options 对象中注入 include_usage
// 例如: "stream_options": {} → "stream_options": {"include_usage":true}
func injectIntoStreamOptions(body []byte) []byte {
	// 找到 "stream_options" 的位置
	idx := bytes.Index(body, []byte(`"stream_options"`))
	if idx < 0 {
		return body
	}

	// 找到 stream_options 对应的 { 位置
	rest := body[idx:]
	openBrace := bytes.IndexByte(rest, '{')
	if openBrace < 0 {
		return body // stream_options 值为 null 或其他,跳过
	}

	// 计算 { 在 body 中的绝对位置
	absPos := idx + openBrace + 1 // +1 跳过 {

	// 检查是否是空对象 {}
	closeBrace := bytes.IndexByte(rest[openBrace:], '}')
	if closeBrace < 0 {
		return body // 格式异常
	}

	// 判断是否为空对象
	if closeBrace == 1 {
		// 空对象 {},在 { 后直接注入
		result := make([]byte, 0, len(body)+30)
		result = append(result, body[:absPos]...)
		result = append(result, []byte(`"include_usage":true}`)...)
		result = append(result, body[absPos+1:]...)
		return result
	}

	// 非空对象,在最后一个 } 前注入
	// 找到对应的闭合 }
	absCloseBrace := idx + openBrace + closeBrace
	result := make([]byte, 0, len(body)+35)
	result = append(result, body[:absCloseBrace]...)
	result = append(result, []byte(`,"include_usage":true}`)...)
	result = append(result, body[absCloseBrace+1:]...)
	return result
}

// appendStreamOptions 在 JSON 末尾追加 stream_options 字段
// 例如: {...,"messages":[...]} → {...,"messages":[...],"stream_options":{"include_usage":true}}
func appendStreamOptions(body []byte) []byte {
	// 去除末尾空白
	trimmed := bytes.TrimRight(body, " \t\n\r")
	if len(trimmed) == 0 || trimmed[len(trimmed)-1] != '}' {
		return body // 格式异常,回退到原始 body
	}

	// 在最后一个 } 前插入
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
