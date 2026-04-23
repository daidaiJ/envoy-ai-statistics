package usage

import (
	"context"
	"sync"
)

// requestCtxKey 用于在 context 中存储 RequestCtx 的 key
type requestCtxKey struct{}

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

// NewRequestCtx 创建新的请求上下文
func NewRequestCtx() *RequestCtx {
	return &RequestCtx{}
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