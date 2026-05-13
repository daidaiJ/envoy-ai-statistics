# LLM Ext-Proc 服务

基于 Envoy ext_proc 的 LLM API 请求统计服务，用于拦截和分析推理服务的 POST 流量，提取关键信息（model、path、Authorization）并统计 token usage。

## 功能特性

- **路径过滤**：只统计 `/v1/chat/completions`、`/v1/messages`、`/v1/embeddings` 路径
- **流式支持**：自动识别流式（SSE）和非流式响应
- **Usage 解析**：从 SSE 数据中提取 token 使用量
- **流式请求体注入**：自动为流式请求注入 `stream_options={"include_usage": true}`，强制后端返回 usage（无需客户端修改）
- **延迟指标**：自动采集请求总耗时 (duration) 和首 token 延迟 (TTFT)，聚合后推送至 Redis Stream
- **多后端 JSON 解析**：支持三种 JSON 解析策略（build tag 切换），兼顾性能与兼容性
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
│   │   ├── context.go       # RequestCtx 请求上下文定义（含延迟指标）
│   │   ├── processor.go     # Processor 接口 + RouterProcessor 实现
│   │   └── sse.go           # SSE 响应解析 + usage 提取
│   ├── resp/
│   │   ├── usage_raw.go     # UsageRaw 定义 + ParseUsage 入口
│   │   ├── parse_default.go # json-iterator 全量反序列化（默认）
│   │   ├── parse_gjson.go   # gjson 路径提取（build tag: gjson）
│   │   ├── parse_sonic.go   # sonic JIT 解析（build tag: sonicjson）
│   │   ├── responser.go     # Responser 接口（保留扩展）
│   │   └── testdata/        # 测试数据 + golden 基准文件（gitignore）
│   ├── aggregator/          # 时间窗口聚合器，推送 Redis Stream
│   └── util/
│       └── headers.go       # HeaderMap 提取工具函数
├── pkg/
│   ├── json/                # JSON 库 build tag 切换封装
│   ├── logger/              # 动态日志等级控制（slog 标准库）
│   ├── server/
│   │   ├── server.go        # ExtProcServer + gRPC stream 处理
│   │   ├── health.go        # gRPC 健康检查
│   │   └── loglevel.go      # HTTP 日志等级 API
│   └── redis/               # Redis 客户端封装
└── scripts/
    ├── gen_testdata.py      # 生成 bench 测试数据 + golden 正确性基准
    ├── run_bench.sh         # 运行 benchmark + 正确性验证，生成报告
    └── install_tools.sh     # 安装 bench 依赖工具
```

## JSON 解析后端

项目支持三种 JSON 解析策略，通过 Go build tag 在编译时切换，运行时零开销。

### 切换方式

```bash
# 默认：json-iterator（全量反序列化）
go build ./cmd

# gjson（路径提取）
go build -tags gjson ./cmd

# sonic（JIT 加速）
go build -tags sonicjson ./cmd
```

### 性能对比

> 测试环境：AMD Ryzen 5 4600H / Go 1.26.3 / Linux amd64 / CGO_ENABLED=0
> 测试数据：1000 条 SSE 事件，49MB（small/medium/large/xlarge 混合分布）
> 正确性：三种实现均通过 golden 文件逐条验证（1000/1000 匹配，累加汇总一致）
> 以 **default (json-iterator)** 为基准线 (1.00x)，倍率 >1 表示更快

#### 单条提取（单请求实时路径，`extract()`）

| 指标 | small 事件 (~500B) | | | large 事件 (~50KB) | | |
|------|:---:|:---:|:---:|:---:|:---:|:---:|
| | **default** | **gjson** | **sonic** | **default** | **gjson** | **sonic** |
| 耗时 ns/op | 4,835 | 2,776 | 1,450 | 762,450 | 108,753 | 71,384 |
| **耗时倍率** | 1.00x | **1.74x** ↓ | **3.33x** ↓ | 1.00x | **7.01x** ↓ | **10.7x** ↓ |
| 吞吐 MB/s | 190 | 331 | 635 | 87 | 607 | 925 |
| **吞吐倍率** | 1.00x | **1.74x** | **3.34x** | 1.00x | **7.01x** | **10.7x** |
| 内存 B/op | 2,609 | 216 | 1,145 | 702,685 | 160 | 153,843 |
| **内存倍率** | 1.00x | **0.08x** | **0.44x** | 1.00x | **0.0002x** | **0.22x** |
| allocs/op | 12 | 6 | 3 | 48 | 5 | 5 |

#### 批量提取（预提取全部 JSON，逐条 `extract()` × 1000）

| 指标 | **default** | **gjson** | **sonic** |
|------|:---:|:---:|:---:|
| 耗时 ns/op | 465,601,244 | 68,050,493 | 54,359,101 |
| **耗时倍率** | 1.00x | **6.84x** ↓ | **8.56x** ↓ |
| 吞吐 MB/s | 109 | 743 | 930 |
| **吞吐倍率** | 1.00x | **6.84x** | **8.56x** |
| 内存 B/op | 435,844,790 | 185,292 | 80,522,192 |
| **内存倍率** | 1.00x | **0.0004x** | **0.18x** |
| allocs/op | 25,577 | 5,490 | 4,244 |

> 批量 benchmark 预提取全部 JSON 到 `[][]byte` 后逐条 `extract()`，消除 `ParseUsage` 的 early-return + 行分割开销，
> 纯测 JSON 解析吞吐。旧版 benchmark 直接调用 `ParseUsage`，该函数在首条命中事件即返回，实际只解析了 1 条 JSON。

### 各后端特征

| | default (json-iterator) | gjson | sonic |
|---|---|---|---|
| 实现方式 | 全量反序列化 + sync.Pool | 按路径提取标量字段 | JIT 编译 + 全量反序列化 |
| CGO 依赖 | 无 | 无 | 无（JIT 纯 Go，但需 mmap PROT_EXEC） |
| 平台兼容 | 全平台 | 全平台 | amd64/arm64 Linux/macOS ² |
| 二进制增量 | 基准 | +~200KB | +~2-3MB |
| 核心优势 | 全量结构体，灵活扩展 | 内存极低，大 payload 快 7x | 单条延迟最低 (3~10x) |
| 核心劣势 | 最慢 (1x)，allocs 最多 | 多字段需多次扫描 | 安全策略敏感，平台受限 |

> ² sonic 在不支持 JIT 的平台（arm32、mips、Windows）自动回退到 `encoding/json`，性能大幅下降。
> 严格 seccomp / SELinux 策略可能阻止 mmap(PROT_EXEC)，导致 JIT 初始化失败。

### 场景推荐

| 场景 | 推荐 | 原因 |
|------|------|------|
| **生产环境（通用）** | **sonic** | 单条延迟最低 (3~10x)，批量 8.56x，需确认容器 JIT 安全策略 |
| **高并发 + 小 payload 为主** | **sonic** | 单条 3.33x，allocs 仅 3 次，GC 压力最小 |
| **大 payload / 内存敏感** | **gjson** | 内存仅 default 的 0.02%，大 payload 单条快 7x，批量 6.84x |
| **交叉编译 / 嵌入式** | **gjson** | 纯 Go 无架构限制，性能接近 sonic，内存极低 |
| **json-iterator 兼容需求** | **default** | 全平台兼容，但性能差距明显，建议迁移 |

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
  - `ProcessRequestBody`：**拦截流式请求体，注入 `stream_options={"include_usage": true}`**（字节流直接修改，避免全量 JSON 序列化）
  - `ProcessResponseHeaders`：判断流式/非流式
  - `ProcessResponseBody`：累积响应体，EndOfStream 时解析

### internal/usage/sse.go

SSE 数据处理：

- `recordBodyChunk()`：记录最近 **1 个** body chunk（性能优化，减少内存占用）
- `printRecordedBody()`：EndOfStream 时打印记录内容并解析 usage
- `parseUsageFromSSE()`：解析 SSE 数据，提取 usage 字段
- `maxLen = 102400`：单个 chunk 最大记录长度

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

- 基于 Go 标准库 `slog`，`os.Stdout` 输出
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
| `avg_duration_ms` | 窗口内请求平均总耗时（ms） |
| `max_duration_ms` | 窗口内请求最大总耗时（ms） |
| `avg_ttft_ms` | 窗口内请求平均首 token 延迟（ms） |
| `max_ttft_ms` | 窗口内请求最大首 token 延迟（ms） |

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
  "sent_at": "2024-01-15T10:00:30.000000000Z",
  "inf_svc_id": "svc-inference-01",
  "avg_duration_ms": 1250,
  "max_duration_ms": 3200,
  "avg_ttft_ms": 180,
  "max_ttft_ms": 450
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

详细更新日志请查看 [CHANGELOG.md](./CHANGELOG.md)。

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
| `processing_mode.request_body_mode` | `BUFFERED` | 缓冲请求体（支持拦截注入 `stream_options`） |
| `processing_mode.response_header_mode` | `NONE` | 不发送响应头（减少开销） |
| `processing_mode.response_body_mode` | `STREAMED` | 流式发送响应体（避免 OOM） |
| `failure_mode_allow` | `true` | 服务故障时流量透传 |

## 输出示例

```
[LLM统计] model: [gpt-4], path: [/v1/chat/completions?stream=true], pathOnly: [/v1/chat/completions], sk: [sk-xxx]
[LLM统计] 响应 Content-Type: text/event-stream (流式: true)

========== 响应结束 ==========
响应格式: 流式
--- Chunk  ---
[data: {"choices":[...]}
data: {"usage":{"prompt_tokens":10,"completion_tokens":5}}
data: [DONE]]
============================

========== Usage 信息 ==========
发现 usage: {
  "prompt_tokens": 10,
  "completion_tokens": 5,
  "total_tokens": 15
}
================================
```

## Benchmark 指南

### 生成测试数据

```bash
# 生成 1000 条 SSE 事件（~49MB）+ golden 正确性基准文件
python3 scripts/gen_testdata.py
# 输出:
#   internal/resp/testdata/sse_events.jsonl        — SSE 事件（benchmark 用）
#   internal/resp/testdata/correctness_golden.json  — 每条事件期望解析结果 + 累加汇总
```

### 运行 Benchmark + 正确性验证

```bash
# 一键运行三种后端对比 + 正确性验证，生成 Markdown 报告
scripts/run_bench.sh

# 指定 bench 时间（默认 5s）
scripts/run_bench.sh bench -benchtime=10s

# 多次采样（用于统计分析）
scripts/run_bench.sh bench -benchtime=5s -count=3

# 仅运行正确性验证（不跑 benchmark）
scripts/run_bench.sh verify
```

报告输出到 `build/bench_report_<timestamp>.md`，包含 benchmark 结果和正确性验证表格。

### 正确性验证机制

`gen_testdata.py` 生成的 `correctness_golden.json` 包含每条事件的期望解析结果（model、input、output、cached）和累加汇总。
三种实现的 `TestExtractCorrectness_*` 测试逐条对比 `extract()` 输出与 golden 文件，确保解析结果完全一致。

```bash
# 单独运行某个实现的正确性验证
go test -run TestExtractCorrectness_default -v ./internal/resp/
go test -run TestExtractCorrectness_gjson -v -tags gjson ./internal/resp/
go test -run TestExtractCorrectness_sonic -v -tags sonicjson ./internal/resp/
```

### Benchmark 套件说明

| Benchmark | 说明 |
|-----------|------|
| `BenchmarkExtractBatch_*` | **批量提取**：预提取全部 JSON 到 `[][]byte`，逐条 `extract()`，纯测 JSON 解析吞吐 |
| `BenchmarkExtract_single_*/small` | 单条小事件 (~500B) 提取 |
| `BenchmarkExtract_single_*/large` | 单条大事件 (~50KB) 提取 |
| `BenchmarkParseUsage_*` | 端到端 `ParseUsage()`（含行分割 + early-return），反映真实调用路径 |

### 单独运行某个后端

```bash
# default (json-iterator)
go test -bench=. -benchmem -run=^$ ./internal/resp/

# gjson
go test -bench=. -benchmem -run=^$ -tags gjson ./internal/resp/

# sonic
go test -bench=. -benchmem -run=^$ -tags sonicjson ./internal/resp/
```

### 清理

```bash
scripts/run_bench.sh clean
```