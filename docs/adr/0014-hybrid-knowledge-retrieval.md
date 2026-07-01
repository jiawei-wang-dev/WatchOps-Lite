# ADR 0014: Optional Hybrid Knowledge Retrieval

- Status: Accepted
- Date: 2026-07-01

## Context

BM25 provides strong, explainable retrieval for operational terms, identifiers, and runbook wording. It is weaker when incident questions and knowledge chunks use different language. Vector retrieval can improve semantic recall, but it adds model availability, cost, dimensional compatibility, and migration concerns.

Existing indexed chunks do not contain vectors and must remain usable.

## Decision

Support three configured retrieval modes:

- `bm25`: Elasticsearch text retrieval only
- `vector`: query embedding plus Elasticsearch dense-vector retrieval
- `hybrid`: BM25 and vector candidates fused with reciprocal rank fusion

BM25 remains the default. Embedding generation is behind a provider interface with disabled mode, a deterministic test implementation, and an OpenAI-compatible HTTP provider whose API key is read from a configured environment variable.

When embeddings are enabled, new chunks store dense vectors. Existing chunks without vectors remain valid and BM25-searchable. Vector queries filter for the embedding field.

Hybrid fusion uses RRF because lexical and vector scores are not directly comparable. Each result exposes retrieval mode and available BM25, vector, and RRF scores. If embedding generation or vector search fails and fallback is enabled, hybrid mode returns BM25 evidence with an explicit warning and fallback metadata.

## Consequences

Knowledge ingestion and HTTP search contracts remain backward compatible while gaining optional semantic recall. Deployments can adopt embeddings gradually without reindexing before startup.

Vector-only mode requires a working provider and compatible index mapping. Hybrid mode is resilient to provider failure. BM25 preserves exact operational identifiers and remains the safety net.

Reranking is deferred until eval cases can demonstrate measurable benefit. Future work may add batch backfill, provider-specific rate limiting, and retrieval-quality reports.

## Independence

This design is independently implemented for WatchOps-Lite. It does not copy Pilot project structure, prompts, comments, source code, or documentation.
