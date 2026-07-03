# Checkout 服务高错误率排障 Runbook

## 症状 Symptoms

checkout 服务出现 HTTP 5xx 错误率升高、p95 latency 增长或超时计数持续增加。告警窗口通常与 payment 支付依赖延迟升高重合。

稳定检索关键词：checkout、payment、timeout、error rate、retry amplification、支付依赖、超时、错误率、重试放大。

## 排查步骤 Investigation

1. 检查故障时间范围内 checkout 的请求量、5xx error rate、p95 latency 和 timeout counter。
2. 检索应用日志中的 upstream timeout、context deadline exceeded、连接池耗尽和 retry 消息。
3. 检查慢 checkout Trace，确认 payment authorization 是否占据关键路径。
4. 对齐 payment 依赖延迟、错误率与 checkout 异常窗口，避免只凭单条日志下结论。
5. 检查 retry amplification（重试放大）是否增加流量，并耗尽 checkout worker 或连接池。

## 缓解措施 Mitigation

- 优先降低不安全重试，避免 retry amplification，不能只通过增加 timeout 掩盖问题。
- 如果 payment 依赖确认退化，按批准流程启用 Circuit Breaker（熔断）或限流，并通知 payment 服务负责人。
- 只有部署时间与指标、日志、Trace 证据一致时，才回滚 checkout 版本。
- 所有操作都要保留 request_id、trace_id、证据 ID 和故障时间范围。

## 证据要求 Evidence Expectations

指标、日志、Trace 和 runbook 是不同证据来源。单条 payment timeout 日志不能证明根因；缺少真实 Trace 或依赖指标时，必须明确记录为局限性（Limitations）。
