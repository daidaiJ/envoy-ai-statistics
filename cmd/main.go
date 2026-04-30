package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"tokenusage/config"
	"tokenusage/internal/aggregator"
	"tokenusage/internal/usage"
	"tokenusage/pkg/logger"
	"tokenusage/pkg/server"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8888", "gRPC server address")
	httpAddr := flag.String("http-addr", "0.0.0.0:8889", "HTTP server address for log level control")
	configPath := flag.String("config", "config.yaml", "config file path")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	logger.Init(cfg.Log.Level)

	// 打印配置信息（安全，密码已隐藏）
	logger.Info(cfg.String())

	// 初始化聚合器
	agg, err := aggregator.New(cfg)
	if err != nil {
		logger.Error("create aggregator failed", "error", err)
		os.Exit(1)
	}
	usage.SetAggregator(agg)
	agg.Start()
	defer agg.Stop() // 确保退出时 flush 剩余数据

	// 启动 HTTP 服务（用于动态配置日志等级）
	go func() {
		http.Handle("/log/level", server.NewLogLevelHandler())
		logger.Info("HTTP server started for log level control", "addr", *httpAddr)
		if err := http.ListenAndServe(*httpAddr, nil); err != nil {
			logger.Error("HTTP server error", "error", err)
		}
	}()

	// 启动 gRPC 服务器
	go func() {
		if err := server.StartServer(*addr); err != nil {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// 等待终止信号
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down server...")
}
