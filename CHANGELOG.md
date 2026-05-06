# 更新日志

所有 notable 的变更都会记录在这个文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

---

## [Unreleased]

---

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
