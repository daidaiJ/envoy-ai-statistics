package exporters

import (
	"fmt"

	"tokenusage/config"
	"tokenusage/pkg/logger"
)

// New 根据配置创建对应的 Exporter 实现
func New(cfg *config.Config) (Exporter, error) {
	switch cfg.Exporter.Type {
	case "redis":
		logger.Info("初始化 Redis Stream exporter")
		return NewRedisExporter(cfg)
	case "prometheus":
		logger.Info("初始化 Prometheus exporter")
		return NewPrometheusExporter(cfg)
	default:
		return nil, fmt.Errorf("unsupported exporter type: %q (supported: redis, prometheus)", cfg.Exporter.Type)
	}
}
