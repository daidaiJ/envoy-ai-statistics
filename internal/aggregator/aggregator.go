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
	SK       string
	InfSvcId string
}

// AggregateValue 聚合值
type AggregateValue struct {
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
	Count        int64
	WindowStart  time.Time // 窗口内第一条记录时间
	LastRecorded time.Time // 窗口内最后一条记录时间
	InfSvcId     string
	ModelId      string
}

// record 记录请求
type record struct {
	infSvcId string
	sk       string
	model    string
	input    int64
	output   int64
	cached   int64
}

// Aggregator 时间窗口聚合器
type Aggregator struct {
	config      *config.Config
	redisClient *redisclient.Client

	mu         sync.Mutex
	aggregates map[AggregateKey]*AggregateValue

	ticker    *time.Ticker
	recordCh  chan record
	stopCh    chan struct{}
	wg        sync.WaitGroup
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
		recordCh:    make(chan record, 10000), // 带 buffer 的 channel，避免高并发时阻塞
		stopCh:      make(chan struct{}),
	}, nil
}

// Start 启动定时刷新和 record 消费
func (a *Aggregator) Start() {
	a.ticker = time.NewTicker(a.config.Aggr.WindowDuration)

	// 启动 record 消费 goroutine
	a.wg.Add(1)
	go a.consumeRecords()

	// 启动定时刷新 goroutine
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

// consumeRecords 消费 record channel，批量处理以提高吞吐
func (a *Aggregator) consumeRecords() {
	defer a.wg.Done()

	// 批量处理，减少锁操作次数
	batch := make([]record, 0, 100)

	for {
		select {
		case rec := <-a.recordCh:
			batch = append(batch, rec)
			// 非阻塞继续取，凑满一批
			for len(batch) < cap(batch) {
				select {
				case r := <-a.recordCh:
					batch = append(batch, r)
				default:
					// channel 空了，处理当前批次
					goto process
				}
			}
		process:
			if len(batch) > 0 {
				a.processBatch(batch)
				batch = batch[:0]
			}
		case <-a.stopCh:
			// 停止前消费完剩余记录
			for len(a.recordCh) > 0 {
				batch = append(batch, <-a.recordCh)
			}
			if len(batch) > 0 {
				a.processBatch(batch)
			}
			return
		}
	}
}

// processBatch 批量处理记录（一次加锁）
func (a *Aggregator) processBatch(batch []record) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	for _, rec := range batch {
		key := AggregateKey{SK: rec.sk, InfSvcId: rec.infSvcId}
		if val, exists := a.aggregates[key]; exists {
			val.InputTokens += rec.input
			val.OutputTokens += rec.output
			val.CachedTokens += rec.cached
			val.Count++
			val.LastRecorded = now
		} else {
			a.aggregates[key] = &AggregateValue{
				InputTokens:  rec.input,
				OutputTokens: rec.output,
				CachedTokens: rec.cached,
				Count:        1,
				WindowStart:  now,
				LastRecorded: now,
				InfSvcId:     rec.infSvcId,
				ModelId:      rec.model,
			}
		}
	}
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

// Record 记录一条 usage 数据（异步非阻塞）
func (a *Aggregator) Record(infSvcId, sk, model string, input, output, cached int64) {
	rec := record{
		infSvcId: infSvcId,
		sk:       sk,
		model:    model,
		input:    input,
		output:   output,
		cached:   cached,
	}

	select {
	case a.recordCh <- rec:
		// 成功发送
	default:
		// channel 满了，丢弃记录并记录日志（避免阻塞调用者）
		logger.Warn("record channel full, dropping record",
			"sk", sk,
			"inf_svc_id", infSvcId,
			"model", model,
		)
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
			"model":         val.ModelId,
			"input_tokens":  val.InputTokens,
			"output_tokens": val.OutputTokens,
			"cached_tokens": val.CachedTokens,
			"count":         val.Count,
			"window_start":  val.WindowStart.Format(time.RFC3339Nano),
			"window_end":    val.LastRecorded.Format(time.RFC3339Nano),
			"sent_at":       sentAt.Format(time.RFC3339Nano),
			"inf_svc_id":    key.InfSvcId,
		}

		if err := a.redisClient.XAdd(ctx, a.config.Aggr.StreamKey, fields); err != nil {
			logger.Error("XAdd failed", "error", err, "sk", key.SK, "inf_svc_id", key.InfSvcId)
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
			"inf_svc_id", key.InfSvcId,
			"input_tokens", val.InputTokens,
			"output_tokens", val.OutputTokens,
			"cached_tokens", val.CachedTokens,
			"count", val.Count,
			"model", val.ModelId,
		)
	}
}
