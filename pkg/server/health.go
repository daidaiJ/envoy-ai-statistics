package server

import (
	"context"

	"google.golang.org/grpc/health/grpc_health_v1"
)

// HealthServer 实现 gRPC 健康检查
type HealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
}

// NewHealthServer 创建健康检查服务器
func NewHealthServer() *HealthServer {
	return &HealthServer{}
}

// Check 健康检查接口
func (h *HealthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}