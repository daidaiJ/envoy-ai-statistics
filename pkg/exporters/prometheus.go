package exporters

import (
	"net/http"
	"sync"

	"tokenusage/config"
	"tokenusage/pkg/logger"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// PrometheusExporter 将 usage 指标上报到 Prometheus
//
// 使用自定义 registry，禁用 Go runtime 和 process 默认 collector，
// 仅暴露业务指标：token 计数器 + 请求延迟直方图。
type PrometheusExporter struct {
	mu sync.Mutex
	// buf 聚合窗口内缓冲的记录，Flush 时一次性上报
	buf []Record

	inputTokens  *prometheus.CounterVec
	outputTokens *prometheus.CounterVec
	cachedTokens *prometheus.CounterVec
	requests     *prometheus.CounterVec
	duration     *prometheus.HistogramVec
	ttft         *prometheus.HistogramVec
}

const (
	labelSK     = "sk"
	labelModel  = "model"
	labelInfSvc = "inf_svc_id"
)

// NewPrometheusExporter 创建 Prometheus 导出器并启动 HTTP /metrics 端点
func NewPrometheusExporter(cfg *config.Config) (*PrometheusExporter, error) {
	// 自定义 registry：不注册默认的 Go collector 和 process collector，
	// 仅暴露业务指标
	reg := prometheus.NewRegistry()

	e := &PrometheusExporter{
		buf: make([]Record, 0, 256),

		inputTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_input_tokens_total",
			Help: "Total input (prompt) tokens processed",
		}, []string{labelSK, labelModel, labelInfSvc}),

		outputTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_output_tokens_total",
			Help: "Total output (completion) tokens generated",
		}, []string{labelSK, labelModel, labelInfSvc}),

		cachedTokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_cached_tokens_total",
			Help: "Total cached input tokens (prompt cache hits)",
		}, []string{labelSK, labelModel, labelInfSvc}),

		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "llm_requests_total",
			Help: "Total LLM requests processed",
		}, []string{labelSK, labelModel, labelInfSvc}),

		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "llm_request_duration_seconds",
			Help:    "End-to-end request latency in seconds",
			Buckets: prometheus.ExponentialBuckets(0.05, 2, 12), // 50ms → ~204s
		}, []string{labelSK, labelModel, labelInfSvc}),

		ttft: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "llm_ttft_seconds",
			Help:    "Time to first token (TTFT) in seconds",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 12), // 10ms → ~40s
		}, []string{labelSK, labelModel, labelInfSvc}),
	}

	// 注册业务指标
	reg.MustRegister(
		e.inputTokens,
		e.outputTokens,
		e.cachedTokens,
		e.requests,
		e.duration,
		e.ttft,
	)

	// 启动 HTTP /metrics 端点
	addr := cfg.Exporter.Prometheus.MetricsAddr
	if addr == "" {
		addr = ":9090"
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		DisableCompression: true,
	}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	go func() {
		logger.Info("Prometheus metrics server started", "addr", addr)
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			logger.Error("Prometheus metrics server error", "error", err)
		}
	}()

	return e, nil
}

// Record 缓冲一条记录，Flush 时批量上报
func (e *PrometheusExporter) Record(rec Record) {
	e.mu.Lock()
	e.buf = append(e.buf, rec)
	e.mu.Unlock()
}

// Flush 将缓冲区中的记录批量上报到 Prometheus 计数器/直方图
func (e *PrometheusExporter) Flush() error {
	e.mu.Lock()
	if len(e.buf) == 0 {
		e.mu.Unlock()
		return nil
	}
	batch := e.buf
	e.buf = make([]Record, 0, cap(e.buf))
	e.mu.Unlock()

	for _, rec := range batch {
		labels := prometheus.Labels{
			labelSK:     rec.SK,
			labelModel:  rec.Model,
			labelInfSvc: rec.InfSvcId,
		}

		e.inputTokens.With(labels).Add(float64(rec.InputTokens))
		e.outputTokens.With(labels).Add(float64(rec.OutputTokens))
		e.cachedTokens.With(labels).Add(float64(rec.CachedTokens))
		e.requests.With(labels).Inc()
		e.duration.With(labels).Observe(rec.Duration.Seconds())
		if rec.TTFT > 0 {
			e.ttft.With(labels).Observe(rec.TTFT.Seconds())
		}
	}

	return nil
}

// Close 无资源需要释放
func (e *PrometheusExporter) Close() error {
	return nil
}
