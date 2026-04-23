package server

import (
	"fmt"
	"math"
	"net"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"tokenusage/internal/usage"
)

// ExtProcServer 实现 gRPC stream 处理
type ExtProcServer struct {
	extprocv3.UnimplementedExternalProcessorServer
	processor *usage.RouterProcessor
}

// NewExtProcServer 创建新的 ext_proc 服务器
func NewExtProcServer() *ExtProcServer {
	return &ExtProcServer{
		processor: &usage.RouterProcessor{},
	}
}

// Process 每个请求一个 stream，天然隔离并发
func (s *ExtProcServer) Process(stream extprocv3.ExternalProcessor_ProcessServer) error {
	reqCtx := usage.NewRequestCtx()
	ctx := usage.ContextWithRequestCtx(stream.Context(), reqCtx)

	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}

		var resp *extprocv3.ProcessingResponse

		switch r := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			resp, _ = s.processor.ProcessRequestHeaders(ctx, r.RequestHeaders.Headers)
		case *extprocv3.ProcessingRequest_RequestBody:
			resp, _ = s.processor.ProcessRequestBody(ctx, r.RequestBody)
		case *extprocv3.ProcessingRequest_ResponseHeaders:
			resp, _ = s.processor.ProcessResponseHeaders(ctx, r.ResponseHeaders.Headers)
		case *extprocv3.ProcessingRequest_ResponseBody:
			resp, _ = s.processor.ProcessResponseBody(ctx, r.ResponseBody)
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// StartServer 启动 gRPC 服务器
func StartServer(addr string) error {
	if addr == "" {
		addr = "0.0.0.0:8888"
	}

	listen, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	srv := grpc.NewServer(grpc.MaxRecvMsgSize(math.MaxInt))
	extprocv3.RegisterExternalProcessorServer(srv, NewExtProcServer())
	grpc_health_v1.RegisterHealthServer(srv, NewHealthServer())

	fmt.Printf("ext_proc server started on %s\n", addr)
	return srv.Serve(listen)
}