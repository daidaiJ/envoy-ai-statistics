# LLM Ext-Proc 服务

基于 Envoy ext_proc 的 LLM API 请求统计服务，拦截推理服务的 POST 流量，提取 token usage 并聚合导出。

## 核心特性

- **多后端导出**：可配置 Redis Stream 或 Prometheus 作为导出后端
- **可配置聚合**：时间窗口聚合可开关，关闭时每条记录直接导出
- **SSE Usage 解析**：支持 OpenAI / Anthropic 格式，自动注入 `stream_options` 强制返回 usage
- **延迟指标**：自动采集请求总耗时和首 token 延迟 (TTFT)
- **高性能 JSON 解析**：三种后端（json-iterator / gjson / sonic）通过 build tag 切换
- **故障透传**：服务异常时流量正常转发，不影响业务

## 快速开始

```bash
# 编译（默认 json-iterator）
go build ./cmd

# 使用 sonic 加速
go build -tags sonicjson ./cmd

# 运行
go run ./cmd -addr :8888 -config config.yaml
```

## 配置

```yaml
redis:
  addr: "localhost:6379"
  password: ""
  db: 0

log:
  level: "info"                     # debug | info | warn | error

grpc_max_recv_size: 67108864        # gRPC 最大接收消息大小（字节），默认 64MB
mask_len: 4                         # SK 日志掩码保留尾部字符数，0 全部掩码

stream_options:
  disabled: false                   # 禁用 stream_options 注入
  paths:                            # 注入路径（Anthropic 默认不注入）
    - "/v1/chat/completions"
    - "/v1/completions"
    - "/v1/embeddings"

aggregator:
  enabled: true                     # false 则每条记录直接导出
  window_duration: "30s"
  channel_size: 10000

exporter:
  type: "redis"                     # redis | prometheus
  redis:
    stream_key: "llm:usage"
    stream_max_len: 10000
  prometheus:
    metrics_addr: ":9090"
```

## Exporter

### Redis Stream

聚合后批量 XADD，字段包括 `sk`、`model`、`input_tokens`、`output_tokens`、`cached_tokens`、`count`、`avg_duration_ms`、`avg_ttft_ms` 等。

### Prometheus

自定义 registry，禁用 Go/process collector，仅暴露业务指标：

| 类型 | 指标 | Labels |
|------|------|--------|
| Counter | `llm_input_tokens_total` | sk, model, inf_svc_id |
| Counter | `llm_output_tokens_total` | sk, model, inf_svc_id |
| Counter | `llm_cached_tokens_total` | sk, model, inf_svc_id |
| Counter | `llm_requests_total` | sk, model, inf_svc_id |
| Histogram | `llm_request_duration_seconds` | sk, model, inf_svc_id |
| Histogram | `llm_ttft_seconds` | sk, model, inf_svc_id |

```bash
curl http://localhost:9090/metrics
```

## JSON 解析后端

通过 build tag 编译时切换，运行时零开销：

```bash
go build ./cmd                        # json-iterator（默认）
go build -tags gjson ./cmd            # gjson（内存极低）
go build -tags sonicjson ./cmd        # sonic（延迟最低）
```

> 性能对比和场景推荐见 [bench_report.md](./bench_report.md)

## 项目结构

```
├── cmd/main.go                 # 入口
├── config/config.go            # 配置加载
├── internal/
│   ├── usage/                  # ext_proc 处理：请求头、请求体注入、SSE 解析
│   ├── resp/                   # JSON 解析（三后端 build tag 切换）
│   ├── aggregator/             # 时间窗口聚合器
│   └── util/                   # HeaderMap 提取
├── pkg/
│   ├── exporters/              # Exporter 接口 + Redis / Prometheus 实现
│   ├── logger/                 # 动态日志等级（slog）
│   ├── server/                 # gRPC 服务器 + 健康检查 + 日志等级 API
│   └── redis/                  # Redis 客户端封装
└── scripts/                    # 测试数据生成 + benchmark
```

## 日志

运行时动态调整：

```bash
curl http://localhost:8889/log/level                          # 查看
curl -X PUT http://localhost:8889/log/level -d '{"level":"debug"}'  # 切换
```

## 更新日志

详见 [CHANGELOG.md](./CHANGELOG.md)。
