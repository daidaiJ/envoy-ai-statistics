package server

import (
	"fmt"
	"math"
	"net"

	"tokenusage/internal/usage"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
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
	fmt.Println("[ext_proc] === 新 gRPC stream 连接建立 ===")
	reqCtx := usage.NewRequestCtx()
	ctx := usage.ContextWithRequestCtx(stream.Context(), reqCtx)
	defer reqCtx.Release() // 确保对象放回池中

	for {
		req, err := stream.Recv()
		if err != nil {
			fmt.Printf("[ext_proc] stream 结束: %v\n", err)
			return err
		}

		var resp *extprocv3.ProcessingResponse

		switch r := req.Request.(type) {
		case *extprocv3.ProcessingRequest_RequestHeaders:
			fmt.Println("[ext_proc] 收到 RequestHeaders")
			resp, _ = s.processor.ProcessRequestHeaders(ctx, r.RequestHeaders.Headers)
		case *extprocv3.ProcessingRequest_RequestBody:
			fmt.Println("[ext_proc] 收到 RequestBody")
			resp, _ = s.processor.ProcessRequestBody(ctx, r.RequestBody)
		case *extprocv3.ProcessingRequest_ResponseHeaders:
			fmt.Println("[ext_proc] 收到 ResponseHeaders")
			resp, _ = s.processor.ProcessResponseHeaders(ctx, r.ResponseHeaders.Headers)
		case *extprocv3.ProcessingRequest_ResponseBody:
			resp, _ = s.processor.ProcessResponseBody(ctx, r.ResponseBody)
		default:
			fmt.Printf("[ext_proc] 收到未知请求类型: %T\n", req.Request)
		}

		if err := stream.Send(resp); err != nil {
			fmt.Printf("[ext_proc] 发送响应失败: %v\n", err)
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
