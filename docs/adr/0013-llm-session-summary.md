# ADR 0013: LLM Session Summary with Deterministic Fallback

- Status: Accepted
- Date: 2026-07-01

## Context

Redis session memory retains recent messages and a bounded structured summary. The deterministic summarizer is reliable and dependency-free, but it cannot reliably separate goals, unresolved questions, evidence-supported facts, attempted actions, and important operational identifiers across longer investigations.

Summary generation is context engineering, not Agent answering. It should improve continuity without becoming a new failure mode for Chat.

## Decision

Add an optional LLM implementation behind the existing `session.Summarizer` interface.

The summarizer:

- uses the configured OpenAI-compatible Eino ChatModel
- receives the current structured summary and only the messages leaving the recent window
- uses a versioned JSON-only prompt
- parses exactly `content`, `goal`, `confirmed_facts`, `open_questions`, `attempted_actions`, and `important_entities`
- preserves the current summary version for Redis compare-and-set persistence
- records `summary.llm`, `summary.parse`, and `summary.fallback` spans without message or model-output content

The prompt requires the model to preserve service names, trace IDs, request IDs, evidence IDs, tool names, error codes, and time ranges. User guesses cannot become confirmed facts. Assistant speculation can become confirmed only when the supplied context identifies supporting evidence.

Deterministic summarization remains the default and mandatory fallback. Disabled or incomplete LLM configuration selects it at startup. Runtime model errors, summary timeouts, nil responses, and malformed JSON fall back within the summarizer and do not fail Chat.

## Consequences

LLM summaries can retain richer investigation state without changing Redis keys, ChatService flow, public HTTP contracts, or the Agent input boundary.

The fallback keeps local demos, tests, and degraded environments deterministic. A summary may be less semantically rich during fallback, but session persistence remains available.

The summary model does not answer users, call tools, retrieve new evidence, or reinterpret unsupported claims. Future work may add semantic summary evaluation, token-aware prompt budgets, and dedicated lower-cost model selection.

## Independence

This design is independently implemented for WatchOps-Lite. It does not copy Pilot project structure, prompts, comments, source code, or documentation.
