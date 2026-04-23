# LLM Ext-Proc 服务

基于 Envoy ext_proc 的 LLM API 请求统计服务，用于拦截和分析推理服务的 POST 流量，提取关键信息（model、path、Authorization）并统计 token usage。

## 功能特性

- **路径过滤**：只统计 `/v1/chat/completions`、`/v1/messages`、`/v1/embeddings` 路径
- **流式支持**：自动识别流式（SSE）和非流式响应
- **Usage 解析**：从 SSE 数据中提取 token 使用量
- **故障透传**：ext_proc 服务异常时流量正常转发，不影响业务
- **并发安全**：每个请求独立的 gRPC stream，天然隔离

## 项目结构

```
ext-proc/
├── cmd/main.go              # 入口文件，启动 gRPC 服务器
├── internal/
│   ├── usage/
│   │   ├── context.go       # RequestCtx 请求上下文定义
│   │   ├── processor.go     # Processor 接口 + RouterProcessor 实现
│   │   └── sse.go           # SSE 解析 + body 记录
│   └── util/
│       └── headers.go       # HeaderMap 提取工具函数
├── pkg/server/
│   ├── server.go            # ExtProcServer + gRPC stream 处理
│   └── health.go            # gRPC 健康检查
├── manifests/
│   ├── deployment.yaml      # Kubernetes Deployment + Service
│   └── envoyfilter.yaml     # Istio EnvoyFilter CR
└── Dockerfile               # 构建镜像
```

## 模块说明

### cmd/main.go

程序入口，解析命令行参数并启动 gRPC 服务器。

```bash
# 本地运行
go run ./cmd -addr :8888
```

### internal/usage/context.go

请求上下文定义，存储单个请求全生命周期的数据：

- `RequestCtx` 结构体：包含 Model、Path、ShouldStat、IsStreaming 等字段
- `NewRequestCtx()`：创建新上下文
- `ContextWithRequestCtx()`：将上下文存入 Go context
- `getRequestCtx()`：从 Go context 获取上下文

### internal/usage/processor.go

核心处理逻辑：

- `llmStatPaths`：需要深度统计的路径列表
- `matchLLMPath()`：路径匹配，去掉 query 参数后判断是否需要统计
- `Processor` 接口：定义 ext_proc 四个阶段处理方法
- `RouterProcessor`：LLM 统计处理器实现
  - `ProcessRequestHeaders`：提取 model、path、Authorization
  - `ProcessResponseHeaders`：判断流式/非流式
  - `ProcessResponseBody`：累积响应体，EndOfStream 时解析

### internal/usage/sse.go

SSE 数据处理：

- `recordBodyChunk()`：记录最近 2 个 body chunk（滚动更新）
- `printRecordedBody()`：EndOfStream 时打印记录内容
- `parseUsageFromSSE()`：解析 SSE 数据，提取 usage 字段

### internal/util/headers.go

Envoy HeaderMap 提取工具：

- `GetHeaders()`：从 HeaderMap 批量提取指定 header
- `IsContains()`：判断 map 是否包含指定 key

### pkg/server/server.go

gRPC 服务器：

- `ExtProcServer`：ext_proc gRPC 服务实现
- `Process()`：stream 处理主循环，每个请求独立 stream
- `StartServer()`：启动 gRPC 服务器

### pkg/server/health.go

gRPC 健康检查：

- `HealthServer`：实现标准 gRPC 健康检查协议
- `Check()`：返回 SERVING 状态

## 部署说明

### 1. 构建镜像

```bash
docker build -t llm-ext-proc:latest .
```

### 2. 部署到 Kubernetes

```bash
kubectl apply -f manifests/deployment.yaml
kubectl apply -f manifests/envoyfilter.yaml
```

### 3. 为推理服务添加 label

```bash
kubectl label pod <inference-pod> inference=true
```

## EnvoyFilter 配置说明

| 配置项 | 值 | 说明 |
|--------|-----|------|
| `workloadSelector.labels` | `inference: true` | 只匹配推理服务 |
| `processing_mode.request_header_mode` | `SEND` | 发送请求头 |
| `processing_mode.request_body_mode` | `NONE` | 不发送请求体（减少开销） |
| `processing_mode.response_header_mode` | `NONE` | 不发送响应头（减少开销） |
| `processing_mode.response_body_mode` | `STREAMED` | 流式发送响应体（避免 OOM） |
| `failure_mode_allow` | `true` | 服务故障时流量透传 |

## 输出示例

```
[LLM统计] model: [gpt-4], path: [/v1/chat/completions?stream=true], pathOnly: [/v1/chat/completions], sk: [sk-xxx]
[LLM统计] 响应 Content-Type: text/event-stream (流式: true)

========== 响应结束 ==========
响应格式: 流式
最近的 body chunks:
--- Chunk 1 ---
data: {"choices":[{"delta":{"content":"Hello"}}]}

--- 完整响应体 ---
data: {"choices":[...]}
data: {"usage":{"prompt_tokens":10,"completion_tokens":5}}
data: [DONE]
============================

========== Usage 信息 ==========
发现 usage: {
  "prompt_tokens": 10,
  "completion_tokens": 5,
  "total_tokens": 15
}
================================
```