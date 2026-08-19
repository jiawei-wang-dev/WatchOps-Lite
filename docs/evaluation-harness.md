# Agent Evaluation Harness

WatchOps-Lite evaluates Agent behavior in four layers:

```text
Intent / Slot / Clarification
        -> Retrieval
        -> Tool / Evidence
        -> Agent E2E
```

`make eval` runs the local harness against fixed datasets and writes
`data/eval/report.json`, `data/eval/report.md`, and
`data/eval/bad_cases.json`. It uses the rule recognizer, rule query planner,
offline retrieval corpus, fixture tools, and deterministic Agent runner. It
does not call an external LLM, embedding provider, reranker, Prometheus,
Elasticsearch, or Jaeger.

The 17 scenarios in `testdata/oncall_eval_scenarios.json` contain local demo
fixtures representing Prometheus metrics, Elasticsearch logs, Jaeger traces,
and operational runbooks, plus missing-slot, service-correction, timeout,
empty-result, retrieval-miss, and repeated-call cases. They are synthetic and
must not be described as production data.

Tool expectations use three sets: `required_tools`, `acceptable_tools`, and
`forbidden_tools`. Primary quality metrics are required-tool recall, forbidden
tool violation rate, unnecessary-call rate, and full path validity. Exact tool
set accuracy remains only as a compatibility metric. Arguments are checked by
field semantics (`service`, `time_range`, `trace_id`, `symptom`, and query
meaning), not raw JSON equality.

`testdata/intent_challenge_cases.json` adds 40 deterministic hard cases for
conflicting signals, negation, corrections, multilingual wording, focus reuse,
multi-service dependency requests, and ambiguous missing slots. The suite also
measures avoidable fake-LLM escalation; the fake recognizer is local and never
calls an external model.

Pre-RAG now uses a deterministic conditional Multi-Query decision. Explicit
metrics, logs, trace-ID, simple knowledge, and parameter-correction requests
stay on one authoritative query. Causal/dependency incidents, multiple-symptom
incidents, explicit error chains, and long compound incidents may expand. A
planner error, empty plan, failed original sub-query, or empty merged result
falls back to the authoritative single query.

Multi-Query trigger quality is measured against retrieval value: a correct
trigger improves reciprocal rank over the same case's single-query result, an
incorrect trigger worsens it, and a neutral trigger leaves it unchanged. The
report also separates Recall@5 and MRR for the single-query and triggered
Multi-Query subsets.

## Commands

```bash
make eval
make eval-intent
make eval-retrieval
make eval-replay
make eval-agent
make eval-all
```

`make eval-retrieval-live` preserves the older HTTP/backend retrieval check for
an explicitly running and seeded local stack. It is not part of default
offline evaluation.

Real-model Agent evaluation is opt-in:

```bash
WATCHOPS_EVAL_LLM_ENABLED=true \
WATCHOPS_EVAL_MAX_LLM_CASES=20 \
WATCHOPS_LLM_API_KEY=... \
make eval-agent
```

The command prints the planned case count before execution, caps a run at 30
cases, performs no command-level retry, and skips successfully when the switch
or API key is absent. The local WatchOps server must already be configured for
the intended model and reachable through `WATCHOPS_API_BASE_URL`.

## Bad-case replay

Every failed check becomes a stable, sorted record with `suite`, `case_id`, `stage`,
expected and actual values, reason, optional trace ID, and the dataset's fixed
timestamp. `make eval-replay` loads those IDs and re-executes only matching
cases. Supported stages are intent, slot, clarification, retrieval, tool
selection, tool arguments, evidence, Agent execution, and timeout.

Latency fields are measured wall-clock values. Context efficiency reports
characters, bytes, and message counts; it deliberately reports no token metric
because the project does not currently have one reliable tokenizer shared by
all configured models.

`testdata/eval_bad_case_baseline.json` preserves the nine pre-fix failures and
their root-cause classification. Each report compares that baseline with the
current run and records fixed, still-failing, and newly discovered cases.
