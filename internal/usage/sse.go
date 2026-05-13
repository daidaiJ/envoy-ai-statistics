package usage

import (
	"fmt"
	"time"

	"tokenusage/internal/aggregator"
	"tokenusage/internal/resp"
	"tokenusage/pkg/logger"
)

// defaultAggregator 默认聚合器实例，由 main.go 初始化时设置
var defaultAggregator *aggregator.Aggregator

// SetAggregator 设置聚合器实例
func SetAggregator(agg *aggregator.Aggregator) {
	defaultAggregator = agg
}

const maxLen = 102400

// recordBodyChunk 记录 body 原始数据（滚动更新，只保留最后一块）
//
// body 来自 gRPC protobuf 消息，在当前调用期间有效。
// 由于 printRecordedBody 在同一调用栈中同步处理，无需拷贝。
func (ctx *RequestCtx) recordBodyChunk(body []byte) {
	if len(body) > maxLen {
		body = body[:maxLen]
	}
	ctx.recentChunks = body
	ctx.Count++
	if ctx.Count > 1 {
		ctx.IsStreaming = true
	}
}

// printRecordedBody 在 EndOfStream 时解析并记录 usage
func (ctx *RequestCtx) printRecordedBody() {
	streamType := "非流式"
	if ctx.IsStreaming {
		streamType = "流式"
	}
	logger.Debug("响应结束", "format", streamType, "path", ctx.Path, "sk", ctx.SK)

	usage := resp.ParseUsage(ctx.recentChunks, ctx.Path)
	if usage != nil && usage.InputToken > 0 {
		// 计算延迟指标
		now := time.Now()
		duration := now.Sub(ctx.StartTime)
		var ttft time.Duration
		if !ctx.FirstChunkTime.IsZero() {
			ttft = ctx.FirstChunkTime.Sub(ctx.StartTime)
		}

		logger.Debug("Usage统计",
			"model", usage.Model,
			"input_tokens", usage.InputToken,
			"output_tokens", usage.OutputToken,
			"cached_tokens", usage.CachedToken,
			"duration_ms", duration.Milliseconds(),
			"ttft_ms", ttft.Milliseconds(),
			"sk", ctx.SK,
			"maas-inference-service", ctx.InferenceId,
		)
		if defaultAggregator != nil {
			defaultAggregator.Record(ctx.InferenceId, ctx.SK, usage.Model,
				usage.InputToken, usage.OutputToken, usage.CachedToken,
				duration, ttft)
		} else {
			logger.Warn("聚合器未初始化，无法记录usage")
		}
	}

	fmt.Printf("============================\n\n")
}
