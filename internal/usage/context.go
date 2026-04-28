package usage

import (
	"context"
	"sync"
)

// requestCtxKey 用于在 context 中存储 RequestCtx 的 key
type requestCtxKey struct{}

// bodyBuf 保留阈值：超过此大小的 buffer 在放回池时丢弃，避免内存膨胀
const bodyBufMaxRetainSize = 1 << 20 // 1MB

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
	PathOnly    string // 路径部分（不含 query）
	SK          string
	RequestID   string
	IsStreaming bool
	ShouldStat  bool // 是否需要深度统计（路径匹配时为 true）

	// 响应体累积
	bodyBuf []byte
	bodyMu  sync.Mutex

	// 记录最近的 body chunks（最多2个，滚动更新）
	recentChunks [2]string
	chunkIndex   int
}

// NewRequestCtx 从对象池获取请求上下文（已重置）
func NewRequestCtx() *RequestCtx {
	return requestCtxPool.Get().(*RequestCtx)
}

// Release 放回对象池，必须在 stream 结束时调用
// 同步重置字段，开销极小（几个字段赋值），无需异步
func (ctx *RequestCtx) Release() {
	// 重置字符串字段
	ctx.Model = ""
	ctx.Path = ""
	ctx.PathOnly = ""
	ctx.SK = ""
	ctx.RequestID = ""
	ctx.IsStreaming = false
	ctx.ShouldStat = false
	ctx.chunkIndex = 0
	ctx.recentChunks[0] = ""
	ctx.recentChunks[1] = ""

	// bodyBuf: 保留容量但重置长度；过大则丢弃让 GC 回收
	if cap(ctx.bodyBuf) > bodyBufMaxRetainSize {
		ctx.bodyBuf = nil
	} else {
		ctx.bodyBuf = ctx.bodyBuf[:0]
	}

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