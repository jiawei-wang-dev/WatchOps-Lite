# ADR 0005: Elasticsearch Knowledge RAG

- Status: Accepted
- Date: 2026-07-01

## Context

WatchOps-Lite needs a first real knowledge retrieval path for operational runbooks and incident guidance. This phase must remain understandable, testable, and useful without prematurely introducing embeddings or a custom RAG framework.

## Decision

Use Elasticsearch as the knowledge chunk index and use lexical BM25 retrieval first.

Documents enter through the HTTP API as plain-text or Markdown content. The retrieval service validates them and applies deterministic paragraph-first chunking. Paragraphs are merged up to a configurable character limit; an oversized paragraph is split further. Every chunk ID is derived from its document ID and zero-based chunk index, so reprocessing the same document identity produces stable chunk identities.

The index stores:

- `chunk_id` and `document_id` as keywords
- `title` and `content` as searchable text
- `source` as a keyword
- `chunk_index` as an integer
- `metadata` as a dynamic object
- `created_at` as a date

Search uses a bounded Elasticsearch `multi_match` query over `title^2` and `content`, with optional exact metadata filters. The retrieval package owns query construction; Gin handlers and Agent code do not depend on Elasticsearch request details.

The project uses the official Elasticsearch Go client behind a small request executor interface. This keeps connection and timeout policy in the platform layer while allowing store tests to run without an Elasticsearch process.

## Why BM25 First

Operational terms such as service names, error codes, endpoint names, and alert labels often have strong lexical value. BM25 provides a mature baseline with no embedding provider, embedding lifecycle, or vector dimension contract. It also gives the evaluation loop a concrete baseline against which future retrieval changes can be measured.

## Why Embeddings Are Deferred

This phase intentionally excludes embeddings, vector search, hybrid fusion, reranking, and reciprocal rank fusion. Adding them before representative evaluation cases exist would increase operational and testing complexity without evidence that they improve WatchOps-Lite's answers.

## Graceful Degradation

Elasticsearch is disabled by default. When disabled, the application still starts, knowledge HTTP endpoints return a sanitized `503 DEPENDENCY_UNAVAILABLE`, and the Eino `search_knowledge` tool retains deterministic mock behavior.

When Elasticsearch is enabled but unavailable during startup, WatchOps-Lite logs a warning and continues. Real knowledge tool calls attempt Elasticsearch and fall back to mock evidence with an explicit `KNOWLEDGE_FALLBACK` warning. Timeouts and request cancellation are not hidden by fallback.

## Consequences

- Knowledge retrieval now has a real, independently testable backend.
- Full original document content is not stored separately in this phase; metadata lookup is reconstructed from chunks.
- There is no durable ingestion status, deletion workflow, deduplication, file extraction, or authorization policy yet.
- Reindexing a document identity replaces chunks with the same IDs, but removing obsolete trailing chunks requires a future document lifecycle workflow.

## Future Extensions

After retrieval evaluation cases are available, the index and retrieval interfaces can evolve to support:

- embeddings and vector fields
- hybrid lexical/vector retrieval
- reciprocal rank fusion
- reranking
- durable document metadata and ingestion state in MySQL
- access-scope filtering, deletion, and deduplication
