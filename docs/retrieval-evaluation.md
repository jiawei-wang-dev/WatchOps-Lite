# Retrieval Evaluation

WatchOps-Lite uses retrieval evaluation to make knowledge RAG quality explainable without adding another retrieval system, reranker model, vector database, or paid dependency.

## Why chunking is needed

Runbooks and incident notes are longer than a single useful evidence item. Chunking turns a document into smaller passages that can be indexed, scored, cited, and returned as bounded evidence. This helps the Agent reference a specific runbook section instead of treating a whole document as one vague source.

## Why overlap is useful

The current chunker is intentionally simple and paragraph-oriented. In larger corpora, overlap can help preserve context when a relevant sentence sits near a chunk boundary. Overlap is a future tuning lever, not a reason to make the MVP chunker complex.

## BM25, vector, and hybrid retrieval

- BM25 is strong for exact service names, error codes, request IDs, alert names, and operational keywords.
- Vector retrieval is strong for semantic similarity when the query and runbook use different wording.
- Hybrid retrieval combines lexical and semantic signals so exact operational identifiers do not get lost while semantic matches can still surface.

WatchOps-Lite supports BM25-first retrieval and optional vector/hybrid retrieval when embeddings are configured. The retrieval eval runner reports the retrieval mode and score fields that are actually returned. It does not invent vector, hybrid, or rerank scores.

## Why rerank may help

A lightweight deterministic rerank could later use service exact match, title overlap, error-code match, keyword overlap, recency metadata, and existing BM25/vector/hybrid scores. This stage does not add a reranker because the goal is to evaluate current retrieval quality before tuning it.

## Eval cases

Retrieval eval cases live in `testdata/retrieval_eval_cases.json`. Each case records:

- case ID
- query
- optional service
- optional expected document or chunk ID
- expected keywords
- expected source type
- notes

Run locally:

```bash
docker compose up -d --wait
make run CONFIG=configs/config.local.json
./scripts/demo_seed_knowledge.sh
make eval-retrieval
```

The script calls the local knowledge search API and prints:

- case ID and query
- retrieval mode when available
- top-k result IDs
- matched keywords
- hit or miss
- available score fields such as `bm25_score`, `vector_score`, `hybrid_score`, and `rrf_score`
- empty recall behavior
- summary pass rate

Set optional environment variables:

```bash
export WATCHOPS_API_BASE_URL=http://localhost:8080
export WATCHOPS_RETRIEVAL_EVAL_CASES=testdata/retrieval_eval_cases.json
export WATCHOPS_RETRIEVAL_EVAL_TOP_K=5
export WATCHOPS_RETRIEVAL_EVAL_OUTPUT=tmp/retrieval_eval_report.json
```

Generated reports under `tmp/` are local artifacts and should not be committed.

## Empty recall behavior

If retrieval returns no results, WatchOps-Lite should not invent knowledge evidence. Chat responses should include limitations when no knowledge evidence supports a claim. Other evidence tools, such as metrics, logs, traces, alerts, or topology, may still provide useful evidence.

The eval runner marks empty retrieval as `empty_recall=true` so it is visible during demos and interviews.

## Evidence constraints reduce hallucination

The Agent can only support conclusions and inferences with evidence IDs that came from executed tools. If `search_knowledge` returns nothing, there are no knowledge evidence IDs to cite. This makes missing retrieval explicit instead of allowing the model to fabricate a runbook.

## Current limitations

- The default eval runner checks top-k keyword and ID hits; it is not an LLM judge.
- It evaluates the configured local retrieval mode rather than benchmarking every possible mode automatically.
- It does not add a cross-encoder, LLM reranker, new vector database, or external paid service.
- The demo corpus is intentionally small, so some cases are useful as partial-recall or empty-recall probes.

Future improvements can add versioned eval datasets, deterministic rerank experiments, corpus coverage reports, and CI-friendly fixture-backed retrieval tests.
