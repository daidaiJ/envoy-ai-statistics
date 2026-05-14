package aggregator

import (
	"sync"
	"sync/atomic"
	"time"

	"tokenusage/config"
	"tokenusage/pkg/exporters"
	"tokenusage/pkg/logger"
)

// AggregateKey 聚合键：按 sk + model 分组
type AggregateKey struct {
	SK       string
	Model    string
	InfSvcId string
}

// AggregateValue 聚合值
type AggregateValue struct {
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
	Count        int64
	WindowStart  time.Time
	LastRecorded time.Time
	InfSvcId     string
	ModelId      string

	// 延迟指标（聚合窗口内累计，flush 时计算均值/极值）
	DurationSum time.Duration
	DurationMax time.Duration
	TTFTSum     time.Duration
	TTFTMax     time.Duration
}

// Aggregator 时间窗口聚合器
type Aggregator struct {
	config   *config.Config
	exporter exporters.Exporter

	mu         sync.Mutex
	aggregates map[AggregateKey]*AggregateValue

	ticker         *time.Ticker
	recordCh       chan exporters.Record
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
	droppedRecords atomic.Int64
}

// New 创建聚合器
func New(cfg *config.Config, exp exporters.Exporter) (*Aggregator, error) {
	chSize := cfg.Aggr.ChannelSize
	if chSize == 0 {
		chSize = 10000
	}

	return &Aggregator{
		config:     cfg,
		exporter:   exp,
		aggregates: make(map[AggregateKey]*AggregateValue),
		recordCh:   make(chan exporters.Record, chSize),
		stopCh:     make(chan struct{}),
	}, nil
}

// Start 启动定时刷新和 record 消费
func (a *Aggregator) Start() {
	if a.config.Aggr.Enabled {
		a.ticker = time.NewTicker(a.config.Aggr.WindowDuration)
	}

	// 启动 record 消费 goroutine
	a.wg.Add(1)
	go a.consumeRecords()

	// 启动定时刷新 goroutine（仅聚合模式）
	if a.ticker != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for {
				select {
				case <-a.ticker.C:
					a.flush("timer")
				case <-a.stopCh:
					return
				}
			}
		}()
	}
}

// consumeRecords 消费 record channel
func (a *Aggregator) consumeRecords() {
	defer a.wg.Done()

	if !a.config.Aggr.Enabled {
		// 非聚合模式：直接转发到 exporter
		for {
			select {
			case rec := <-a.recordCh:
				a.exporter.Record(rec)
			case <-a.stopCh:
				// 停止前消费完剩余记录
				for len(a.recordCh) > 0 {
					a.exporter.Record(<-a.recordCh)
				}
				return
			}
		}
	}

	// 聚合模式：批量处理
	batch := make([]exporters.Record, 0, 100)

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
func (a *Aggregator) processBatch(batch []exporters.Record) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	for _, rec := range batch {
		key := AggregateKey{SK: rec.SK, Model: rec.Model, InfSvcId: rec.InfSvcId}
		if val, exists := a.aggregates[key]; exists {
			val.InputTokens += rec.InputTokens
			val.OutputTokens += rec.OutputTokens
			val.CachedTokens += rec.CachedTokens
			val.Count++
			val.LastRecorded = now
			val.DurationSum += rec.Duration
			if rec.Duration > val.DurationMax {
				val.DurationMax = rec.Duration
			}
			val.TTFTSum += rec.TTFT
			if rec.TTFT > val.TTFTMax {
				val.TTFTMax = rec.TTFT
			}
		} else {
			a.aggregates[key] = &AggregateValue{
				InputTokens:  rec.InputTokens,
				OutputTokens: rec.OutputTokens,
				CachedTokens: rec.CachedTokens,
				Count:        1,
				WindowStart:  now,
				LastRecorded: now,
				InfSvcId:     rec.InfSvcId,
				ModelId:      rec.Model,
				DurationSum:  rec.Duration,
				DurationMax:  rec.Duration,
				TTFTSum:      rec.TTFT,
				TTFTMax:      rec.TTFT,
			}
		}
	}
}

// Stop 优雅停止，幂等（可多次调用）
func (a *Aggregator) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		if a.ticker != nil {
			a.ticker.Stop()
		}
		a.wg.Wait()

		// 最后一次刷新
		a.flush("shutdown")

		// 打印丢弃统计
		dropped := a.droppedRecords.Load()
		if dropped > 0 {
			logger.Warn("aggregator stopped with dropped records", "dropped", dropped)
		}

		if a.exporter != nil {
			a.exporter.Close()
		}
	})
}

// Record 记录一条 usage 数据（异步非阻塞）
func (a *Aggregator) Record(infSvcId, sk, model string, input, output, cached int64, duration, ttft time.Duration) {
	rec := exporters.Record{
		InfSvcId:     infSvcId,
		SK:           sk,
		Model:        model,
		InputTokens:  input,
		OutputTokens: output,
		CachedTokens: cached,
		Duration:     duration,
		TTFT:         ttft,
	}

	select {
	case a.recordCh <- rec:
	default:
		a.droppedRecords.Add(1)
	}
}

// FlushNow 立即触发一次 flush（供 model 变更等场景调用）
func (a *Aggregator) FlushNow(reason string) {
	a.flush(reason)
}

// flush 推送聚合数据到 exporter
func (a *Aggregator) flush(reason string) {
	if !a.config.Aggr.Enabled {
		// 非聚合模式：调用 exporter.Flush 将缓冲区数据发送
		if err := a.exporter.Flush(); err != nil {
			logger.Error("exporter flush failed", "error", err, "reason", reason)
		}
		return
	}

	a.mu.Lock()
	if len(a.aggregates) == 0 {
		a.mu.Unlock()
		// 即使没有聚合数据，也 flush exporter 自身缓冲区
		if err := a.exporter.Flush(); err != nil {
			logger.Error("exporter flush failed", "error", err, "reason", reason)
		}
		return
	}

	// 取出当前窗口数据
	data := a.aggregates
	a.aggregates = make(map[AggregateKey]*AggregateValue)
	a.mu.Unlock()

	// 转换为 exporter.Record 并发送
	for key, val := range data {
		avgDuration := val.DurationSum / time.Duration(val.Count)
		avgTTFT := val.TTFTSum / time.Duration(val.Count)

		a.exporter.Record(exporters.Record{
			InfSvcId:     key.InfSvcId,
			SK:           key.SK,
			Model:        key.Model,
			InputTokens:  val.InputTokens,
			OutputTokens: val.OutputTokens,
			CachedTokens: val.CachedTokens,
			Duration:     avgDuration,
			TTFT:         avgTTFT,
		})
	}

	// flush exporter 缓冲区
	if err := a.exporter.Flush(); err != nil {
		logger.Error("exporter flush failed", "error", err, "reason", reason)
	}

	// flush 时打印丢弃统计
	dropped := a.droppedRecords.Swap(0)
	if dropped > 0 {
		logger.Warn("records dropped during flush window",
			"dropped", dropped,
			"reason", reason,
		)
	}
}
