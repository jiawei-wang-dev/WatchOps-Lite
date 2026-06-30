# ADR 0006: MySQL Feedback and Eval Seed

- Status: Accepted
- Date: 2026-07-01

## Context

WatchOps-Lite needs to retain explicit user judgments about Chat answers and turn reviewed judgments into reproducible evaluation inputs. Session memory in Redis is intentionally short-lived and Elasticsearch is a retrieval index, so neither is an appropriate durable source for this business data.

This phase needs a small persistence loop without introducing an automatic evaluation platform.

## Decision

Use MySQL through Go's `database/sql` package and the Go MySQL driver. MySQL stores two durable record types:

- `feedback`: upvotes/downvotes, reason tags, comments, corrected answers, answer snapshots, evidence IDs, tool-run summaries, metadata, and creation time
- `eval_cases`: optional source feedback, good/bad case type, input, expected behavior, optional gold answer, forbidden patterns, metadata, and creation time

The platform package configures the connection pool and creates these tables when MySQL is enabled and reachable. Domain services depend on store interfaces rather than `database/sql`.

MySQL is disabled by default. When it is disabled, unavailable stores keep bootstrap wiring complete and feedback/eval endpoints return a structured `503`. When it is enabled but unavailable during startup, WatchOps-Lite logs a warning and continues serving unrelated functionality.

## Feedback-to-Eval Policy

Eval seeding is explicit through `POST /api/v1/eval/cases`; feedback does not automatically become an eval case.

When a source feedback ID is supplied:

- `rating=up` may seed only `good_case`
- `rating=down` may seed only `bad_case`

A downvoted answer snapshot is retained as diagnostic context, not silently promoted to a gold answer. The creator must state expected behavior and may provide a separately reviewed gold answer.

This preserves both sides of the future regression loop:

- good cases protect evidence-backed behavior that should continue working
- bad cases capture missing evidence, unsupported claims, poor tool selection, or weak limitations that should not recur

## Why No Automatic Eval Runner

This phase establishes durable inputs only. Automatic execution, LLM judging, scoring, prompt comparison, dashboards, background workers, and automated promotion would require additional policies for redaction, review, versioning, cost, and false-positive handling. Those concerns should be designed against a real set of reviewed cases rather than guessed upfront.

## Consequences

- Feedback and eval seeds survive process and Redis expiry.
- Stored answer/evidence/tool snapshots provide reproduction context.
- The application remains usable when MySQL is disabled or temporarily unavailable.
- Schema initialization is intentionally small and is not yet a general migration framework.
- Long-term memory, document metadata, audit records, review state, redaction, JSON export, and the eval runner remain deferred.

## Future Use

Reviewed cases can later be redacted and exported to `agent_eval_cases.json`. A future regression runner can reuse the same good and bad cases across prompt versions, retrieval strategies, tool routing, or end-to-end Agent behavior without changing the feedback API.
