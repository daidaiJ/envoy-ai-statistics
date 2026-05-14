package exporters

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tokenusage/config"
	"tokenusage/pkg/logger"
	redisclient "tokenusage/pkg/redis"
)

// RedisExporter 将聚合后的 usage 推送到 Redis Stream
type RedisExporter struct {
	mu          sync.Mutex
	client      *redisclient.Client
	cfg         config.RedisConfig
	streamKey   string
	maxLen      int64
	buf         []Record
}

// NewRedisExporter 创建 Redis Stream 导出器
func NewRedisExporter(cfg *config.Config) (*RedisExporter, error) {
	client, err := redisclient.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("redis connect: %w", err)
	}

	return &RedisExporter{
		client:    client,
		cfg:       cfg.Redis,
		streamKey: cfg.Exporter.Redis.StreamKey,
		maxLen:    cfg.Exporter.Redis.StreamMaxLen,
		buf:       make([]Record, 0, 256),
	}, nil
}

// Record 缓冲一条记录，等待 Flush 时批量发送
func (e *RedisExporter) Record(rec Record) {
	e.mu.Lock()
	e.buf = append(e.buf, rec)
	e.mu.Unlock()
}

// Flush 将缓冲区数据推送到 Redis Stream
func (e *RedisExporter) Flush() error {
	e.mu.Lock()
	if len(e.buf) == 0 {
		e.mu.Unlock()
		return nil
	}
	batch := e.buf
	e.buf = make([]Record, 0, cap(e.buf))
	e.mu.Unlock()

	ctx := context.Background()
	sentAt := time.Now()

	for _, rec := range batch {
		fields := map[string]interface{}{
			"sk":              rec.SK,
			"model":           rec.Model,
			"input_tokens":    rec.InputTokens,
			"output_tokens":   rec.OutputTokens,
			"cached_tokens":   rec.CachedTokens,
			"count":           int64(1),
			"sent_at":         sentAt.Format(time.RFC3339Nano),
			"inf_svc_id":      rec.InfSvcId,
			"avg_duration_ms": rec.Duration.Milliseconds(),
			"max_duration_ms": rec.Duration.Milliseconds(),
			"avg_ttft_ms":     rec.TTFT.Milliseconds(),
			"max_ttft_ms":     rec.TTFT.Milliseconds(),
		}

		if err := e.client.XAdd(ctx, e.streamKey, fields, e.maxLen); err != nil {
			logger.Error("XAdd failed", "error", err, "sk", rec.SK, "inf_svc_id", rec.InfSvcId)
			// 失败的记录放回缓冲区
			e.mu.Lock()
			e.buf = append([]Record{rec}, e.buf...)
			e.mu.Unlock()
			continue
		}

		logger.Info("XAdd success",
			"sk", rec.SK,
			"inf_svc_id", rec.InfSvcId,
			"model", rec.Model,
			"input_tokens", rec.InputTokens,
			"output_tokens", rec.OutputTokens,
			"count", 1,
		)
	}
	return nil
}

// Close 关闭 Redis 连接
func (e *RedisExporter) Close() error {
	if e.client != nil {
		return e.client.Close()
	}
	return nil
}
