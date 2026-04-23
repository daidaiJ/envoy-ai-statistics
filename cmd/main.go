package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"tokenusage/pkg/server"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8888", "gRPC server address")
	flag.Parse()

	// 启动服务器
	go func() {
		if err := server.StartServer(*addr); err != nil {
			fmt.Printf("server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// 等待终止信号
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	fmt.Println("shutting down server...")
}