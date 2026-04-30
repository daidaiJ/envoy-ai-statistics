# LLM Ext-Proc 服务

基于 Envoy ext_proc 的 LLM API 请求统计服务，用于拦截和分析推理服务的 POST 流量，提取关键信息（model、path、Authorization）并统计 token usage。

## 功能特性

- **路径过滤**：只统计 `/v1/chat/completions`、`/v1/messages`、`/v1/embeddings` 路径
- **流式支持**：自动识别流式（SSE）和非流式响应
- **Usage 解析**：从 SSE 数据中提取 token 使用量
- **故障透传**：ext_proc 服务异常时流量正常转发，不影响业务
- **并发安全**：每个请求独立的 gRPC stream，天然隔离
- **动态日志**：运行时可通过 HTTP API 动态调整日志等级

## 项目结构

```
ext-proc/
├── cmd/main.go              # 入口文件，启动 gRPC + HTTP 服务器
├── config/config.go         # 配置加载 + 安全打印
├── internal/
│   ├── usage/
│   │   ├── context.go       # RequestCtx 请求上下文定义
│   │   ├── processor.go     # Processor 接口 + RouterProcessor 实现
│   │   └── sse.go           # SSE 解析 + body 记录
│   ├── aggregator/          # 时间窗口聚合器，推送 Redis Stream
│   └── util/
│       └── headers.go       # HeaderMap 提取工具函数
├── pkg/
│   ├── logger/              # 动态日志等级控制（slog + zerolog）
│   ├── server/
│   │   ├── server.go        # ExtProcServer + gRPC stream 处理
│   │   ├── health.go        # gRPC 健康检查
│   │   └── loglevel.go      # HTTP 日志等级 API
│   └── redis/               # Redis 客户端封装
├── scripts/debug.sh         # 容器内日志等级控制脚本
├── manifests/
│   ├── deployment.yaml      # Kubernetes Deployment + Service
│   └── envoyfilter.yaml     # Istio EnvoyFilter CR
└── Dockerfile               # 构建镜像
```

## 模块说明

### cmd/main.go

程序入口，解析命令行参数并启动 gRPC 服务器和 HTTP 服务。

```bash
# 本地运行
go run ./cmd -addr :8888 -http-addr :8889

# 参数说明
# -addr       : gRPC 服务地址（默认 0.0.0.0:8888）
# -http-addr  : HTTP 服务地址，用于日志等级控制（默认 0.0.0.0:8889）
# -config     : 配置文件路径（默认 config.yaml）
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

### internal/aggregator/aggregator.go

时间窗口聚合器，将 usage 数据聚合后推送到 Redis Stream：

- `AggregateKey`：聚合键（SK + Model 组合）
- `AggregateValue`：聚合值（token 数量、请求次数、时间窗口）
- `Record()`：非阻塞记录 usage 数据到内存聚合表
- `flush()`：定时推送聚合数据到 Redis Stream
- `Start()/Stop()`：启动定时器、优雅停止（确保剩余数据 flush）

### pkg/logger/logger.go

动态日志等级控制：

- 基于 slog 标准接口 + zerolog ConsoleWriter
- 支持 caller 信息（文件名:行号）
- 时区从 `TZ` 或 `TIMEZONE` 环境变量加载
- `SetLevel()`：运行时动态切换等级

### pkg/server/loglevel.go

HTTP 日志等级 API：

- `GET /log/level`：获取当前日志等级
- `PUT /log/level`：设置日志等级

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

## Usage 聚合机制

### 设计目的

将 LLM API 的 token usage 数据按时间窗口聚合后批量推送至 Redis Stream，避免每个请求单独推送带来的性能开销。

### 工作流程

```
请求到达 → 解析 usage → 内存聚合表 → 定时 flush → Redis Stream
                                           ↑
                                    window_duration
```

1. **内存聚合**：按 `SK + Model` 组合为键，在内存中累积 token 数量和请求次数
2. **定时推送**：每隔 `window_duration`（默认 30s）触发一次 flush
3. **Redis Stream**：推送聚合后的数据到 `stream_key`（默认 `llm:usage`）
4. **优雅停止**：服务关闭时确保剩余数据 flush 完成

### 配置项

```yaml
aggregator:
  stream_key: "llm:usage"      # Redis Stream Key
  window_duration: "30s"       # 聚合窗口时长
```

### Redis Stream 数据结构

每次 flush 推送一条消息，包含以下字段：

| 字段 | 说明 |
|------|------|
| `sk` | API Key（Authorization 中提取） |
| `model` | 模型名称 |
| `input_tokens` | 窗口内累计输入 token |
| `output_tokens` | 窗口内累计输出 token |
| `cached_tokens` | 窗口内累计缓存 token |
| `count` | 窗口内请求次数 |
| `window_start` | 窗口内第一条记录时间（RFC3339Nano） |
| `window_end` | 窗口内最后一条记录时间（RFC3339Nano） |
| `sent_at` | 推送时间（RFC3339Nano） |
| `inf_svc_id` | 推理服务 ID（从 maas-inference-service header 提取） |

### 示例输出

```
# Info 级别日志（每次推送）
XAdd success sk=sk-abc123 model=gpt-4 input_tokens=150 output_tokens=80 cached_tokens=20 count=5

# Redis Stream 消息内容
{
  "sk": "sk-abc123",
  "model": "gpt-4",
  "input_tokens": 150,
  "output_tokens": 80,
  "cached_tokens": 20,
  "count": 5,
  "window_start": "2024-01-15T10:00:00.123456789Z",
  "window_end": "2024-01-15T10:00:25.987654321Z",
  "sent_at": "2024-01-15T10:00:30.000000000Z"
}
```

### 失败重试机制

- **Redis 断联重试**：底层 go-redis 客户端配置 `MaxRetries=3`，自动重试连接
- **推送失败保护**：flush 失败的数据放回下一个窗口继续累积，确保不丢失
- **优雅停止**：服务关闭时执行最后一次 flush，确保所有数据推送完成

### 并发安全

- 内存聚合表使用 `sync.Mutex` 保护
- `Record()` 操作异步非阻塞：通过 buffered channel 解耦，避免锁阻塞调用方
- 单一消费者 goroutine 串行处理记录，最小化锁竞争
- flush 时取出当前窗口数据后立即释放锁，新请求写入新的聚合表
- channel 满时丢弃记录并记录日志，保证服务不阻塞

### 更新日志

#### 2025-04-30

- **Record 改为 channel 异步模式**：避免高并发时锁阻塞响应处理
  - 新增 `recordCh`（容量 10000）作为异步缓冲
  - `Record()` 使用 `select + default` 非阻塞发送，channel 满时丢弃记录
  - 单独的 `consumeRecords` goroutine 消费记录，串行更新 map
  - **批量处理优化**：凑满 100 条或 channel 空时批量处理，一次加锁处理多条记录
  - `Stop()` 时消费完剩余记录，确保优雅关闭
- **支持推理服务 ID**：聚合键增加 `inf_svc_id` 字段，从 `maas-inference-service` header 提取

## 日志控制

### 日志等级分布

| 场景 | 等级 |
|------|------|
| 新请求用量、sk、path 等详细信息 | Debug |
| 响应 SSE chunk 详情 | Debug |
| XAdd 推送聚合数据到 Redis | Info |
| 服务启动/关闭 | Info |
| JSON 解析失败、请求上下文缺失 | Warn |
| XAdd 推送失败、响应发送失败 | Error |

### 配置文件

在 `config.yaml` 中设置初始日志等级：

```yaml
log:
  level: "info"  # debug, info, warn, error
```

### 动态调整（HTTP API）

服务启动时默认在 `8889` 端口提供 HTTP API：

```bash
# 获取当前日志等级
curl http://localhost:8889/log/level

# 开启 debug 模式
curl -X PUT http://localhost:8889/log/level -d '{"level":"debug"}'

# 关闭 debug 模式
curl -X PUT http://localhost:8889/log/level -d '{"level":"info"}'
```

### 容器内调试脚本

容器内内置 `debug.sh` 脚本：

```bash
# 进入容器
docker exec -it <container> sh

# 开启 debug 模式（输出详细请求信息）
./debug.sh on

# 关闭 debug 模式
./debug.sh off

# 查看当前日志等级
./debug.sh status
```

可通过环境变量 `HTTP_ADDR` 指定服务地址：

```bash
HTTP_ADDR=10.0.0.1:8889 ./debug.sh on
```

## EnvoyFilter 配置说明
> 老版本的istio 1.18 的使用[1.18适配版本](./manifests/envoyfilter_v1.18.yaml)

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