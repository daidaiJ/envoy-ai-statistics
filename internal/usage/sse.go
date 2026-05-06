package usage

import (
	"bytes"
	"fmt"

	"tokenusage/internal/aggregator"
	"tokenusage/internal/resp"
	"tokenusage/pkg/json"
	"tokenusage/pkg/logger"
)

// defaultAggregator 默认聚合器实例，由 main.go 初始化时设置
var defaultAggregator *aggregator.Aggregator

// SetAggregator 设置聚合器实例
func SetAggregator(agg *aggregator.Aggregator) {
	defaultAggregator = agg
}

const maxLen = 102400

// recordBodyChunk 记录 body 原始字符串（最多两个，滚动更新）
func (ctx *RequestCtx) recordBodyChunk(body []byte) {

	if len(body) > maxLen {
		body = body[:maxLen]
	}
	bodybuf := bytes.Clone(body)
	ctx.recentChunks = bodybuf
	ctx.Count++
	if ctx.Count > 1 {
		ctx.IsStreaming = true
	}
}

// printRecordedBody 在 EndOfStream 时打印记录的 body 内容
func (ctx *RequestCtx) printRecordedBody() {

	streamType := "非流式"
	if ctx.IsStreaming {
		streamType = "流式"
	}

	logger.Debug("响应结束", "format", streamType, "path", ctx.Path, "sk", ctx.SK)

	fmt.Printf("--- Chunk  ---\n[%s]\n---------", string(ctx.recentChunks))
	ctx.parseUsageFromSSE(ctx.recentChunks)

	fmt.Printf("============================\n\n")
}

// parseUsageFromSSE 从 SSE 数据中解析带 usage 字段的报文
func (ctx *RequestCtx) parseUsageFromSSE(body []byte) {

	lines := bytes.Split(body, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		jsonPart := bytes.TrimPrefix(line, []byte("data: "))

		if bytes.Equal(bytes.TrimSpace(jsonPart), []byte("[DONE]")) {
			continue
		}

		chunk := resp.GetResponser(ctx.Path)
		if err := json.Unmarshal(jsonPart, chunk); err != nil {
			logger.Warn("解析响应JSON失败", "error", err, "path", ctx.Path, "raw", string(jsonPart))
			resp.PutResponser(chunk)
			continue
		}

		if chunk.GetInputToken() > 0 {
			logger.Debug("Usage统计",
				"model", chunk.GetModel(),
				"input_tokens", chunk.GetInputToken(),
				"output_tokens", chunk.GetOutputToken(),
				"cached_tokens", chunk.GetCachedToken(),
				"sk", ctx.SK,
				"maas-inference-service", ctx.InferenceId,
			)
			// 调用聚合器记录 usage
			if defaultAggregator != nil {
				defaultAggregator.Record(ctx.InferenceId, ctx.SK, chunk.GetModel(),
					chunk.GetInputToken(), chunk.GetOutputToken(), chunk.GetCachedToken())
			} else {
				logger.Warn("聚合器未初始化，无法记录usage")
			}
		}
		resp.PutResponser(chunk)
	}

}
