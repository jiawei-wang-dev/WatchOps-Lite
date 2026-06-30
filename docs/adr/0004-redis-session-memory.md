# ADR 0004: Redis Session Memory

- Status: Accepted
- Date: 2026-06-30

## Context

The deterministic Chat skeleton handles one request at a time but previously had no session continuity. Sending every historical message to a future model would create an unbounded context, while discarding all older turns would lose confirmed facts and investigative state.

Phase 4 needs a bounded short-term memory model before introducing a real LLM.

## Decision

Redis stores two forms of session context:

- Recent raw messages in a sliding window
- A rolling structured summary of messages that leave the window

The Redis keys are:

```text
session:{session_id}:recent
session:{session_id}:summary
```

### Recent Messages

`recent` is a Redis list containing JSON-encoded messages in chronological order. Every append:

1. Pushes the new message.
2. Trims the list to the configured window.
3. Refreshes the configured session TTL.

The default recent window is 12 messages.

Messages preserve:

- Role
- Content
- Creation time
- Request ID
- Metadata needed for later summarization

### Rolling Summary

`summary` is a Redis hash containing:

- Serialized structured summary data
- Current version

Summary updates use Redis `WATCH` and a transaction. The caller supplies the expected version; a concurrent update produces `ErrVersionConflict` instead of silently overwriting newer state.

The default summary threshold is 12 messages. The default TTL for both keys is 24 hours.

The summary fields are:

- Goal
- Confirmed facts
- Open questions
- Attempted actions
- Important entities
- Human-readable summary content

### Deterministic Placeholder Summarizer

This phase does not call an LLM. A deterministic summarizer processes messages leaving the recent window and preserves:

- Service and resource identifiers
- Trace IDs
- Request IDs
- Time ranges
- Tool error codes
- Tool names and attempted actions
- A detectable user goal

Summary content and structured lists are bounded to avoid replacing one unbounded context with another. A future summary model may improve language quality while preserving the same storage contract and optimistic-version behavior.

### Chat Integration

Before Agent execution, the Chat service loads the summary and recent messages and passes them through `AgentInput`.

After Agent execution, the Chat service:

1. Builds the current user and compact assistant messages.
2. Determines which preloaded messages will leave the sliding window.
3. Merges those messages into the rolling summary.
4. Updates the summary using the version loaded at the start of the request.
5. Appends the user and assistant messages, allowing Redis to trim the recent window.

The summary is committed before trimming recent messages. If summary persistence fails, the request degrades without deleting history that has not yet been summarized.

The Agent never reads Redis directly.

### Graceful Degradation

Redis is not required for the HTTP server to start. The client is created without an eager startup `PING`.

If session context cannot be loaded or saved:

- The Chat request still completes using the current message.
- The response contains `SESSION_MEMORY_UNAVAILABLE`.
- Response metadata marks session memory unavailable.
- Raw Redis errors are not exposed.
- Writes are skipped after a load failure to avoid repeated dependency delays.

## Consequences

### Positive

- Recent conversation remains available in original form.
- Older context has a bounded representation.
- Concurrent summary writes do not silently overwrite each other.
- Chat remains available during a Redis outage.
- The Agent remains independent of Redis.

### Trade-offs

- The placeholder summary is structural rather than fluent.
- Two message appends are not an atomic conversation-turn write.
- A concurrent summary conflict degrades the current request's memory update.
- Session continuity is eventually consistent during failures.

## Testing

- Application tests use the `session.Store` interface with fakes.
- Deterministic summarization is tested without Redis.
- Redis command behavior is covered by optional integration tests that start an isolated local `redis-server` when the binary is available and otherwise skip.

No pre-existing Redis process is required for the normal test command.

## Deferred Work

- LLM-based summary model
- Distributed locking beyond optimistic summary versions
- Durable long-term memory in MySQL
- Elasticsearch knowledge retrieval
- Full Eino ReAct/Graph context reasoning
- OpenTelemetry and Jaeger spans

## Independence

This memory design is independently implemented for WatchOps-Lite and does not copy Pilot project structure, prompts, comments, source code, or documentation.
