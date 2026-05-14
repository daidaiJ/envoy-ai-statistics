# 更新日志

所有 notable 的变更都会记录在这个文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

---

## [Unreleased]

---

## [1.4.0] - 2026-05-14

### Added

- **可配置 Exporter 模块**（`pkg/exporters/`）
  - `Exporter` 接口：`Record()` / `Flush()` / `Close()`
  - Redis Stream 导出器：聚合后批量 XADD，支持 `stream_key` 和 `stream_max_len` 配置
  - Prometheus 导出器：自定义 registry，禁用 Go runtime / process collector，仅暴露业务指标
    - Counters：`llm_input_tokens_total`、`llm_output_tokens_total`、`llm_cached_tokens_total`、`llm_requests_total`
    - Histograms：`llm_request_duration_seconds`、`llm_ttft_seconds`
    - Labels：`sk`、`model`、`inf_svc_id`
    - 自动启动 HTTP `/metrics` 端点（可配置地址）
  - 工厂函数按 `exporter.type` 配置创建对应实现
  - 新增依赖：`github.com/prometheus/client_golang v1.23.2`

- **可配置聚合开关**（`aggregator.enabled`）
  - `false` 时每条记录直接通过 Exporter 导出，不做时间窗口聚合
  - 默认 `true`（向后兼容）

- **Model 变更感知**（`internal/usage/sse.go`）
  - 响应分块中通过 `resp.ExtractModel()` 快速提取 model 字段
  - 检测 model 变更时触发 `aggregator.FlushNow()`，避免跨 model 聚合

- **SK 掩码**（`mask_len` 配置项）
  - 日志中 SK 按配置保留末尾 n 个字符，其余替换为 `***`
  - 默认 `0`（全部掩码）

- **可配置 stream_options 注入**
  - `stream_options.disabled`：禁用注入
  - `stream_options.paths`：指定需要注入的路径列表（默认 OpenAI 兼容路径）
  - Anthropic `/v1/messages` 默认不在注入列表中

- **gRPC 接收大小可配置**（`grpc_max_recv_size`）
  - 默认 64MB，替代原 `math.MaxInt` 无限制

- **gRPC MaxRecvMsgSize 可配置**（`grpc_max_recv_size`）

- **响应体 model 字段快速提取**（`internal/resp/ExtractModel()`）
  - 字节级扫描，不依赖 JSON 解析器，适合高频调用

### Changed

- **聚合器重构**（`internal/aggregator/aggregator.go`）
  - 聚合器不再直接持有 Redis 客户端，改为通过 `Exporter` 接口导出
  - 聚合键从 `{SK, InfSvcId}` 改为 `{SK, Model, InfSvcId}`
  - 新增 `FlushNow(reason)` 方法供 model 变更等场景调用
  - `Stop()` 使用 `sync.Once` 保证幂等，多次调用不再 panic
  - channel 满时丢弃记录不再每条打日志，改为 flush 时打印累计丢弃计数
  - `flush()` 新增 `reason` 参数，区分 timer / shutdown / model_changed

- **请求头竞态修复**（`internal/usage/processor.go`）
  - 移除包级 `reqHeaders` 共享 map
  - `ProcessRequestHeaders` 使用请求局部 map，消除并发竞态和请求串号风险
  - `RouterProcessor` 新增 `*config.Config` 字段，通过 `NewRouterProcessor(cfg)` 创建

- **SSE 处理清理**（`internal/usage/sse.go`）
  - 移除 `fmt.Printf("============================")` 调试输出
  - 日志中 SK 使用 `maskSK()` 掩码
  - 优先使用 `usage.Model` 覆盖 chunk 解析的 model

- **请求上下文**（`internal/usage/context.go`）
  - `Release()` 新增 `ctx.Count = 0` 重置，修复对象池复用时 Count 继承问题

- **Header 提取大小写不敏感**（`internal/util/headers.go`）
  - `GetHeaders()` 对 key 做 `strings.ToLower` 后匹配

- **gRPC 服务器**（`pkg/server/server.go`）
  - `StartServer` 接收 `*config.Config`，`MaxRecvMsgSize` 从配置读取
  - `Process()` 显式处理 processor error，自动回退透传响应
  - default 分支和 nil resp 安全回退为 passthrough 响应

- **Redis Ping 超时**（`pkg/redis/client.go`）
  - Ping 使用 `context.WithTimeout(5s)`，避免启动时网络阻塞

- **JSON 解析失败日志**（`internal/resp/usage_raw.go`）
  - 不再输出 raw body，仅记录 path 和 length

- **移除未使用依赖**
  - `go mod tidy` 清理 `github.com/stretchr/testify`

### Removed

- `internal/util.IsContains()` 函数（合并到 `GetHeaders` 内部）
- `sse.go` 中的 `fmt.Printf` 调试输出
- `sse.go` 中的日志输出完整 SK（改为掩码）

## [1.3.0] - 2026-05-13

### Added

- **多后端 JSON 解析（build tag 切换）**
  - `internal/resp/parse_default.go`：json-iterator 全量反序列化 + sync.Pool（默认，无 build tag）
  - `internal/resp/parse_gjson.go`：gjson 按路径提取标量字段（`-tags gjson`），零结构体分配
  - `internal/resp/parse_sonic.go`：sonic JIT 加速反序列化（`-tags sonicjson`）
  - `pkg/json/json_sonic.go`：`pkg/json` 包的 sonic 实现（`-tags sonicjson`）
  - 三种方案通过 `//go:build` 条件编译互斥，运行时零开销
  - 新增依赖：`github.com/tidwall/gjson v1.19.0`、`github.com/bytedance/sonic v1.15.1`

- **延迟指标采集与推送**
  - `RequestCtx` 新增 `StartTime`（请求头到达时间）和 `FirstChunkTime`（首个响应 body chunk 到达时间）
  - `ProcessRequestHeaders` 记录 `StartTime`
  - `ProcessResponseBody` 在首个 chunk 到达时记录 `FirstChunkTime`（TTFT）
  - `Aggregator.Record()` 新增 `duration` 和 `ttft` 参数
  - `AggregateValue` 新增 `DurationSum/DurationMax/TTFTSum/TTFTMax` 累计字段
  - Redis Stream flush 时新增 `avg_duration_ms`、`max_duration_ms`、`avg_ttft_ms`、`max_ttft_ms` 四个字段

- **Benchmark 基础设施**
  - `internal/resp/bench_default_test.go`：json-iterator 方案 benchmark
  - `internal/resp/bench_gjson_test.go`：gjson 方案 benchmark（`-tags gjson`）
  - `internal/resp/bench_sonic_test.go`：sonic 方案 benchmark（`-tags sonicjson`）
  - `internal/resp/bench_helper_test.go`：benchmark 共享辅助函数（`findEventBySize`、`stripSSEPrefix`）
  - `scripts/gen_testdata.py`：生成 1000 条 SSE 测试数据（small/medium/large/xlarge 四档，OpenAI + Anthropic 格式各 50%）
  - `scripts/run_bench.sh`：一键运行三种后端对比 benchmark，生成 Markdown 报告
  - `scripts/install_tools.sh`：安装 bench 依赖工具

### Changed

- **Usage 解析重构（`internal/resp/`）**
  - 新增 `internal/resp/usage_raw.go`：`UsageRaw` 轻量结构体 + `ParseUsage()` 统一入口
  - `ParseUsage()` 替代原 `sse.go` 中的 `parseUsageFromSSE()`，职责从 usage 包移至 resp 包
  - `extract()` 函数由三种后端各自实现（build tag 互斥），返回 `*UsageRaw`
  - `internal/resp/responser.go`：移除对象池和 `GetResponser/PutResponser`，仅保留 `Responser` 接口定义

- **SSE 解析简化（`internal/usage/sse.go`）**
  - `recordBodyChunk` 不再 `bytes.Clone`，直接引用 gRPC protobuf 消息的 `[]byte`（同一调用栈内有效）
  - `recentChunks` 从 `[2][]byte` 滚动改为单个 `[]byte`
  - 移除 `parseUsageFromSSE()`，改为调用 `resp.ParseUsage()`
  - 移除 `fmt.Printf` 调试输出，仅保留 `logger.Debug` 结构化日志

- **SSE 解析辅助函数纯 Go 化（`internal/resp/usage_raw.go`）**
  - `splitLines()`：手写分割，避免 `bytes.Split` 额外分配
  - `trimSpace()`：手写裁剪，避免 `bytes.TrimSpace` 分配
  - `trimDataPrefix()`：手写 SSE `data: ` 前缀裁剪

### Removed

- **移除部署相关文件**（Dockerfile、manifests/、scripts/debug.sh）
- **移除 zerolog 及其间接依赖**（`github.com/rs/zerolog`、`github.com/mattn/go-colorable`、`github.com/mattn/go-isatty`）
- **移除 stretchr/testify 间接依赖**

## [1.2.0] - 2026-05-06

### Added

- **流式请求体注入 `stream_options={"include_usage": true}`**
  - 新增 `ProcessRequestBody` 方法，拦截并修改流式请求体
  - 使用字节流直接修改，避免对长上下文请求做全量 JSON 反序列化/序列化
  - 自动检测 `"stream": true` 后注入 `stream_options.include_usage`，强制后端返回 usage
  - 支持三种场景：
    - 无 `stream_options` → 在 JSON 末尾追加
    - 有 `stream_options` 但无 `include_usage` → 在对象内注入
    - 已有 `include_usage` → 无需修改
  - 非流式请求自动跳过，不修改请求体

### Changed

- **性能优化：流式请求体只保留最后一个分块**
  - `RequestCtx.recentChunks` 从 `[2][]byte` 改为 `[]byte`，减少内存占用
  - `recordBodyChunk` 不再滚动保留 2 个 chunk，只保留最后一个
  - 同步请求体也能兼容解析（通过 `Count` 字段判断流式）

- **EnvoyFilter 请求体模式改为 BUFFERED**
  - `request_body_mode` 从 `NONE` 改为 `BUFFERED`，支持请求体拦截
  - 对应文件：`manifests/envoyfilter_v1.18.yaml`

- **SSE 解析优化**
  - `maxLen` 从 1024 增加到 102400，支持更长的响应体记录
  - 去掉 `data: ` 前缀检查，更灵活地解析 SSE 数据

- **日志优化**
  - 移除 zerolog 依赖，统一使用 `slog` + `os.Stdout`
  - `ProcessRequestHeaders` 日志从 `Debug` 改为 `Info`
  - 无 `SK` 的请求不统计（`ShouldStat = false`）

### Removed

- 移除 `pkg/logger/logger.go` 中的 zerolog 相关代码（`zerolog.ConsoleWriter`、`toZerologLevel` 等）

---

## [1.1.0] - 2025-04-30

### Added

- **Record 改为 channel 异步模式**：避免高并发时锁阻塞响应处理
  - 新增 `recordCh`（容量 10000）作为异步缓冲
  - `Record()` 使用 `select + default` 非阻塞发送，channel 满时丢弃记录
  - 单独的 `consumeRecords` goroutine 消费记录，串行更新 map
  - **批量处理优化**：凑满 100 条或 channel 空时批量处理，一次加锁处理多条记录
  - `Stop()` 时消费完剩余记录，确保优雅关闭

- **支持推理服务 ID**：聚合键增加 `inf_svc_id` 字段，从 `maas-inference-service` header 提取

---

## [1.0.0] - 2025-04-xx

### Added

- 初始版本发布
- 基于 Envoy ext_proc 的 LLM API 请求统计服务
- 支持路径过滤、流式/非流式响应识别、Usage 解析
- 时间窗口聚合器，批量推送至 Redis Stream
- 动态日志等级控制（HTTP API）
- Istio EnvoyFilter CR 部署配置
