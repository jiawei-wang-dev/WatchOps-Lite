# ADR 0015: Rule-based Eval Case Runner

- Status: Accepted
- Date: 2026-07-01

## Context

Feedback can already seed reusable good and bad eval cases, but stored cases do not prevent regressions until they are executed. A model-based judge would add cost, nondeterminism, and another prompt/version lifecycle before the project has a stable rule baseline.

## Decision

Implement a synchronous, bounded, rule-based eval runner.

Each selected case executes through the existing ChatService boundary. The runner checks:

- evidence and tool runs when required
- limitations, conclusions, and recommendations when requested by metadata
- case-insensitive forbidden patterns
- unexpected tool errors for good cases

Bad and good cases require evidence and tool runs by default. `expected_behavior` remains human-readable review context; the runner does not pretend to understand arbitrary natural-language expectations.

Run summaries are persisted in `eval_runs`. Each `eval_case_results` row stores pass/fail, failure reasons, request ID, trace ID, duration, and creation time. HTTP endpoints create runs and retrieve summaries or results.

## Consequences

Downvoted feedback can become a deterministic regression case with an auditable result. The same Agent, tools, evidence constraints, and fallback policies used by Chat are exercised by eval runs.

Synchronous execution is intentionally limited and suitable for the local portfolio demo. Large suites, background workers, release-to-release comparison, prompt A/B testing, and LLM-as-judge remain deferred.

## Independence

This design is independently implemented for WatchOps-Lite. It does not copy Pilot project structure, prompts, comments, source code, or documentation.
