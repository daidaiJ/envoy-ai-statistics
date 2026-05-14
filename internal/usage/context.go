package usage

import (
	"context"
	"sync"
	"time"
)

// requestCtxKey 用于在 context 中存储 RequestCtx 的 key
type requestCtxKey struct{}

// requestCtxPool 复用 RequestCtx 对象，减少 GC 压力
var requestCtxPool = sync.Pool{
	New: func() any {
		return &RequestCtx{}
	},
}

// RequestCtx 存储单个请求全生命周期的数据
type RequestCtx struct {
	Model       string
	Path        string
	PathOnly    string // 路径部分（不含 query）以防万一
	SK          string
	InferenceId string // 推理服务id
	IsStreaming bool
	ShouldStat  bool // 是否需要深度统计（路径匹配时为 true）

	// 记录最近的 body chunks（最多2个，滚动更新）
	recentChunks []byte
	chunkIndex   int
	Count        int

	// 延迟指标
	StartTime      time.Time // 请求头到达时间
	FirstChunkTime time.Time // 首个响应 body chunk 到达时间（TTFT）
}

// NewRequestCtx 从对象池获取请求上下文（已重置）
func NewRequestCtx() *RequestCtx {
	return requestCtxPool.Get().(*RequestCtx)
}

// Release 放回对象池，必须在 stream 结束时调用
func (ctx *RequestCtx) Release() {
	ctx.Model = ""
	ctx.Path = ""
	ctx.PathOnly = ""
	ctx.SK = ""
	ctx.InferenceId = ""
	ctx.IsStreaming = false
	ctx.ShouldStat = false
	ctx.Count = 0
	ctx.chunkIndex = 0
	ctx.recentChunks = nil
	ctx.StartTime = time.Time{}
	ctx.FirstChunkTime = time.Time{}

	requestCtxPool.Put(ctx)
}

// getRequestCtx 从 context 中获取请求上下文
func getRequestCtx(ctx context.Context) *RequestCtx {
	if reqCtx, ok := ctx.Value(requestCtxKey{}).(*RequestCtx); ok {
		return reqCtx
	}
	return nil
}

// ContextWithRequestCtx 将 RequestCtx 存入 context
func ContextWithRequestCtx(ctx context.Context, reqCtx *RequestCtx) context.Context {
	return context.WithValue(ctx, requestCtxKey{}, reqCtx)
}
