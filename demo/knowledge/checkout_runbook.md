# Checkout Service High Error Rate Runbook

## Symptoms

The checkout service may report a high HTTP 5xx error rate, increased p95 latency, or a growing number of request timeouts. Alerts often coincide with slower calls to the payment dependency.

## Investigation

1. Check checkout request rate, 5xx error rate, p95 latency, and timeout counters for the affected interval.
2. Compare application logs for upstream timeout, context deadline exceeded, connection pool exhaustion, and retry messages.
3. Inspect traces with slow checkout spans and identify whether payment authorization dominates the critical path.
4. Compare payment dependency latency and error rate with the checkout anomaly window.
5. Confirm whether retries amplify traffic or exhaust checkout worker and connection pools.

## Mitigation

Reduce unsafe retry amplification before increasing timeouts. If the payment dependency is degraded, apply the approved circuit-breaker or traffic-management procedure and notify the payment service owner. Roll back a recent checkout release only when deployment timing and evidence support that action.

## Evidence Expectations

Treat metrics, logs, and traces as separate evidence sources. Do not declare the payment dependency as the root cause from a timeout message alone. Record missing telemetry as a limitation and preserve the incident time range in every query.
