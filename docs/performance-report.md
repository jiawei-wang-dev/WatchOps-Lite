# Local Agent Benchmark and Performance Report

## Purpose

WatchOps-Lite includes a small black-box benchmark for local Agent backend validation and interview demonstrations. It calls the existing Chat API with six realistic reliability questions and measures the responses visible to any API client.

This is not a production load test, capacity plan, or QPS claim. It intentionally uses a small sequential sample so a developer can spot behavioral or latency regressions after changing prompts, tools, retrieval, or runtime controls.

## Run It

Start the local dependencies:

```bash
docker compose up -d --wait
```

Start WatchOps-Lite in another terminal:

```bash
make run CONFIG=configs/config.local.json
```

Seed the demo knowledge, logs, metrics, and traces when real backend evidence is desired, then run:

```bash
make benchmark-agent
```

The command prints a summary table and writes:

- `tmp/agent_benchmark_report.json`
- `tmp/agent_benchmark_report.md`

The generated `tmp/` directory is ignored by Git. Override the API URL or output location with `WATCHOPS_API_BASE_URL` and `WATCHOPS_AGENT_BENCHMARK_OUTPUT_DIR`.

## Cases

The versioned cases in `testdata/agent_benchmark_cases.json` cover:

1. High checkout error rate
2. Payment timeout and checkout failures
3. Slow trace analysis
4. Checkout runbook search
5. Active checkout alerts
6. Checkout service topology

Each run uses isolated benchmark session IDs and a current 20-minute time window. The tool and Agent behavior remains unchanged; the benchmark only observes public responses.

## Measurements

The report records:

- Total, successful, and failed requests plus success rate
- Average, p95 nearest-rank, minimum, and maximum request latency
- Average tool-run and evidence counts
- Detectable fallback count
- Limitation and empty-evidence counts
- Request ID and trace ID presence rates
- One optional SSE check with event count and `final_answer` detection

Fallback detection uses existing response metadata and limitations. It is deliberately best-effort: the benchmark does not add public fields merely to make measurement easier.

## Interpretation

- High latency can come from the model provider, external observability backends, retrieval, or multiple sequential tool calls.
- More tool calls commonly increase latency, but can also produce a better-supported answer.
- A detected fallback means an existing safety path handled a dependency, tool, or model-output problem; it is not automatically a failed request.
- Empty evidence should coincide with explicit limitations. It must not be interpreted as evidence of a root cause.
- Missing request or trace IDs reduces diagnosability even when the HTTP request succeeds.

Compare reports generated on the same machine, configuration, model, seeded data, and dependency state. Otherwise, differences are not attributable to a code change alone.

## Known Limitations

- Local-machine timings do not represent production throughput or concurrency.
- Six sequential cases are enough for a smoke benchmark, not statistical capacity analysis.
- Mock and deterministic fallback paths can be much faster than real services.
- Model-provider and Docker resource latency varies between runs.
- The benchmark checks response shape and measurable signals; it is not an LLM quality judge.
- p95 is reported for completeness, but is coarse with this intentionally small sample.

## Interview Talking Points

- WatchOps-Lite is not only functionally tested; it has a repeatable local black-box benchmark.
- The report makes latency, success rate, tool cost, evidence availability, fallback behavior, limitations, and trace visibility discussable with concrete local measurements.
- The benchmark helps detect when prompt, retrieval, tool, or runtime changes degrade Agent behavior without changing the public API.
- Results are presented honestly as local measurements, not as production-readiness claims.
