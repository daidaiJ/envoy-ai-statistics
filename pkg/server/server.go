package server

import (
	"fmt"
	"net"

	"tokenusage/config"
	"tokenusage/internal/usage"
	"tokenusage/pkg/logger"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// ExtProcServer 实现 gRPC stream 处理
type ExtProcServer struct {
	extprocv3.UnimplementedExternalProcessorServer
	processor usage.Processor
}

// NewExtProcServer 创建新的 ext_proc 服务器
func NewExtProcServer(cfg *config.Config) *ExtProcServer {
	return &ExtProcServer{
		processor: usage.NewRouterProcessor(cfg),
	}
}

// Process 每个请求一个 stream，天然隔离并发
func (s *ExtProcServer) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	logger.Debug("新gRPC stream连接建立")
	reqCtx := usage.NewRequestCtx()
	ctx := usage.ContextWithRequestCtx(stream.Context(), reqCtx)
	defer reqCtx.Release()

	// 透传响应模板，用于错误回退和未知消息类型
	passthrough := func() *extprocv3.ProcessingResponse {
		return &extprocv3.ProcessingResponse{
			Response: &extprocv3.ProcessingResponse_RequestHeaders{},
		}
	}

	for {
		req, err := stream.Recv()
		if err != nil {
			logger.Debug("stream结束", "error", err)
			return err
		}

		var resp *extprocv3.ProcessingResponse

		switch r := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			logger.Debug("收到RequestHeaders")
			resp, err = s.processor.ProcessRequestHeaders(ctx, r.RequestHeaders.Headers)
		case *extprocv3.ProcessingRequest_RequestBody:
			logger.Debug("收到RequestBody")
			resp, err = s.processor.ProcessRequestBody(ctx, r.RequestBody)
		case *extprocv3.ProcessingRequest_ResponseHeaders:
			logger.Debug("收到ResponseHeaders")
			resp, err = s.processor.ProcessResponseHeaders(ctx, r.ResponseHeaders.Headers)
		case *extprocv3.ProcessingRequest_ResponseBody:
			resp, err = s.processor.ProcessResponseBody(ctx, r.ResponseBody)
		default:
			logger.Warn("收到未知请求类型", "type", fmt.Sprintf("%T", req.Request))
			resp = passthrough()
		}

		if err != nil {
			logger.Error("processor error, falling back to passthrough", "error", err)
			resp = passthrough()
		}
		if resp == nil {
			resp = passthrough()
		}

		if err := stream.Send(resp); err != nil {
			logger.Error("发送响应失败", "error", err)
			return err
		}
	}
}

// StartServer 启动 gRPC 服务器
func StartServer(addr string, cfg *config.Config) error {
	if addr == "" {
		addr = "0.0.0.0:8888"
	}

	listen, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	srv := grpc.NewServer(grpc.MaxRecvMsgSize(cfg.GRPCMaxRecvSize))
	extprocv3.RegisterExternalProcessorServer(srv, NewExtProcServer(cfg))
	grpc_health_v1.RegisterHealthServer(srv, NewHealthServer())

	logger.Info("ext_proc server started", "addr", addr, "max_recv_size", cfg.GRPCMaxRecvSize)
	return srv.Serve(listen)
}
