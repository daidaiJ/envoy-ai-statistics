package exporters

import "time"

// Record 单次请求的 token 用量记录
type Record struct {
	InfSvcId    string
	SK          string
	Model       string
	InputTokens int64
	OutputTokens int64
	CachedTokens int64
	Duration    time.Duration
	TTFT        time.Duration
}

// Exporter 数据导出接口
//
// 实现方负责将 Record 落盘或上报，聚合逻辑由调用方（aggregator）处理。
type Exporter interface {
	// Record 记录一条 usage（实现方可选缓冲）
	Record(rec Record)
	// Flush 将缓冲区数据批量发送
	Flush() error
	// Close 关闭连接/释放资源
	Close() error
}
