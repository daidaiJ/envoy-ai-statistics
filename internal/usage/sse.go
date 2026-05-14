package usage

import (
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
// 同时从 chunk 中快速提取 model 字段：若 model 发生变更，触发聚合器 mid-window flush。
func (ctx *RequestCtx) recordBodyChunk(body []byte) {
	if len(body) > maxLen {
		body = body[:maxLen]
	}
	ctx.recentChunks = body
	ctx.Count++
	if ctx.Count > 1 {
		ctx.IsStreaming = true
	}

	// 从 chunk 中快速提取 model，检测变更
	if model := resp.ExtractModel(body); model != "" && model != ctx.Model {
		if ctx.Model != "" && defaultAggregator != nil {
			logger.Debug("model 变更，触发 mid-window flush", "old", ctx.Model, "new", model)
			defaultAggregator.FlushNow("model_changed")
		}
		ctx.Model = model
	}
}

// printRecordedBody 在 EndOfStream 时解析并记录 usage
func (ctx *RequestCtx) printRecordedBody() {
	streamType := "非流式"
	if ctx.IsStreaming {
		streamType = "流式"
	}
	logger.Debug("响应结束", "format", streamType, "path", ctx.Path, "sk", maskSK(ctx.SK, 0))

	usage := resp.ParseUsage(ctx.recentChunks, ctx.Path)
	if usage != nil && usage.InputToken > 0 {
		now := time.Now()
		duration := now.Sub(ctx.StartTime)
		var ttft time.Duration
		if !ctx.FirstChunkTime.IsZero() {
			ttft = ctx.FirstChunkTime.Sub(ctx.StartTime)
		}

		// 以 chunk 解析到的 model 为准，优先使用 usage 中的 model
		model := ctx.Model
		if usage.Model != "" {
			model = usage.Model
		}

		logger.Debug("Usage统计",
			"model", model,
			"input_tokens", usage.InputToken,
			"output_tokens", usage.OutputToken,
			"cached_tokens", usage.CachedToken,
			"duration_ms", duration.Milliseconds(),
			"ttft_ms", ttft.Milliseconds(),
			"maas-inference-service", ctx.InferenceId,
		)
		if defaultAggregator != nil {
			defaultAggregator.Record(ctx.InferenceId, ctx.SK, model,
				usage.InputToken, usage.OutputToken, usage.CachedToken,
				duration, ttft)
		} else {
			logger.Warn("聚合器未初始化，无法记录usage")
		}
	}
}
