package aggregator

import (
	"context"
	"sync"
	"time"

	"tokenusage/config"
	"tokenusage/pkg/logger"
	redisclient "tokenusage/pkg/redis"
)

// AggregateKey 聚合键：按 sk + model 分组
type AggregateKey struct {
	SK    string
	Model string
}

// AggregateValue 聚合值
type AggregateValue struct {
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
	Count        int64
	WindowStart  time.Time // 窗口内第一条记录时间
	LastRecorded time.Time // 窗口内最后一条记录时间
}

// Aggregator 时间窗口聚合器
type Aggregator struct {
	config      *config.Config
	redisClient *redisclient.Client

	mu         sync.RWMutex
	aggregates map[AggregateKey]*AggregateValue

	ticker *time.Ticker
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New 创建聚合器
func New(cfg *config.Config) (*Aggregator, error) {
	redisClient, err := redisclient.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Aggregator{
		config:      cfg,
		redisClient: redisClient,
		aggregates:  make(map[AggregateKey]*AggregateValue),
		stopCh:      make(chan struct{}),
	}, nil
}

// Start 启动定时刷新
func (a *Aggregator) Start() {
	a.ticker = time.NewTicker(a.config.Aggr.WindowDuration)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			select {
			case <-a.ticker.C:
				a.flush()
			case <-a.stopCh:
				return
			}
		}
	}()
}

// Stop 优雅停止，刷新剩余数据
func (a *Aggregator) Stop() {
	close(a.stopCh)
	if a.ticker != nil {
		a.ticker.Stop()
	}
	a.wg.Wait()

	// 最后一次刷新
	a.flush()

	if a.redisClient != nil {
		a.redisClient.Close()
	}
}

// Record 记录一条 usage 数据（非阻塞）
func (a *Aggregator) Record(sk, model string, input, output, cached int64) {
	key := AggregateKey{SK: sk, Model: model}

	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	if val, exists := a.aggregates[key]; exists {
		val.InputTokens += input
		val.OutputTokens += output
		val.CachedTokens += cached
		val.Count++
		val.LastRecorded = now
	} else {
		a.aggregates[key] = &AggregateValue{
			InputTokens:  input,
			OutputTokens: output,
			CachedTokens: cached,
			Count:        1,
			WindowStart:  now,
			LastRecorded: now,
		}
	}
}

// flush 推送聚合数据到 Redis Stream
func (a *Aggregator) flush() {
	a.mu.Lock()
	if len(a.aggregates) == 0 {
		a.mu.Unlock()
		return
	}

	// 取出当前窗口数据
	data := a.aggregates
	a.aggregates = make(map[AggregateKey]*AggregateValue)
	a.mu.Unlock()

	// 推送到 Redis
	ctx := context.Background()
	sentAt := time.Now()

	for key, val := range data {
		fields := map[string]interface{}{
			"sk":            key.SK,
			"model":         key.Model,
			"input_tokens":  val.InputTokens,
			"output_tokens": val.OutputTokens,
			"cached_tokens": val.CachedTokens,
			"count":         val.Count,
			"window_start":  val.WindowStart.Format(time.RFC3339Nano),
			"window_end":    val.LastRecorded.Format(time.RFC3339Nano),
			"sent_at":       sentAt.Format(time.RFC3339Nano),
		}

		if err := a.redisClient.XAdd(ctx, a.config.Aggr.StreamKey, fields); err != nil {
			logger.Error("XAdd failed", "error", err, "sk", key.SK, "model", key.Model)
			// 推送失败，数据放回下一个窗口
			a.mu.Lock()
			if existing, ok := a.aggregates[key]; ok {
				existing.InputTokens += val.InputTokens
				existing.OutputTokens += val.OutputTokens
				existing.CachedTokens += val.CachedTokens
				existing.Count += val.Count
			} else {
				a.aggregates[key] = val
			}
			a.mu.Unlock()
			continue
		}

		logger.Info("XAdd success",
			"sk", key.SK,
			"model", key.Model,
			"input_tokens", val.InputTokens,
			"output_tokens", val.OutputTokens,
			"cached_tokens", val.CachedTokens,
			"count", val.Count,
		)
	}
}
