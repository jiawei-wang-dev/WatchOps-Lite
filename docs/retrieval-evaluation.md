# Retrieval Evaluation

WatchOps-Lite uses retrieval evaluation to make knowledge RAG and rerank quality explainable without requiring another vector database or paid dependency.

## Why chunking is needed

Runbooks and incident notes are longer than a single useful evidence item. Chunking turns a document into smaller passages that can be indexed, scored, cited, and returned as bounded evidence. This helps the Agent reference a specific runbook section instead of treating a whole document as one vague source.

## Why overlap is useful

The current chunker is intentionally simple and paragraph-oriented. In larger corpora, overlap can help preserve context when a relevant sentence sits near a chunk boundary. Overlap is a future tuning lever, not a reason to make the MVP chunker complex.

## BM25, vector, and hybrid retrieval

- BM25 is strong for exact service names, error codes, request IDs, alert names, and operational keywords.
- Vector retrieval is strong for semantic similarity when the query and runbook use different wording.
- Hybrid retrieval combines lexical and semantic signals so exact operational identifiers do not get lost while semantic matches can still surface.

WatchOps-Lite supports BM25-first retrieval and optional vector/hybrid retrieval when embeddings are configured. These stages maximize recall; they do not guarantee that the first retrieved chunks are the most useful context for the final answer.

## Why rerank is separate

Passing raw top-k recall directly to the model can waste context on near-duplicates or broad lexical matches. WatchOps-Lite retrieves a larger `candidate_k` set, then reranks before returning the final `top_k`.

The local default is a deterministic rule-based reranker. It uses the base retrieval score, exact service and operational-identifier matches, title/content keyword overlap, runbook preference, optional recency, and an empty-content penalty. This makes ordering explainable and testable.

An optional external provider can be selected with `WATCHOPS_RERANK_PROVIDER=external`. It uses a bounded HTTP `/rerank` request and a key read from the environment variable named by `WATCHOPS_RERANK_API_KEY_ENV`. Timeout, invalid/empty output, missing credentials, or provider failure falls back to the rule-based reranker. The result metadata records only the provider, rerank score/reason, and safe fallback reason.

```bash
export WATCHOPS_RERANK_ENABLED=true
export WATCHOPS_RERANK_PROVIDER=external
export WATCHOPS_RERANK_BASE_URL=https://your-provider.example/v1
export WATCHOPS_RERANK_MODEL=your-rerank-model
export WATCHOPS_RERANK_API_KEY=replace-me
```

The base URL receives a `/rerank` suffix and must expose the configured bounded request/response contract. Secrets remain in environment variables.

## Eval cases

Retrieval eval cases live in `testdata/retrieval_eval_cases.json`. Each case records:

- case ID
- query
- optional service
- optional expected document or chunk ID
- optional multiple relevant document or chunk IDs
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
- available score fields such as `bm25_score`, `vector_score`, `hybrid_score`, `rrf_score`, and `rerank_score`
- rerank provider, deterministic reason, and fallback reason when present
- empty recall behavior
- summary pass rate
- HitRate@K, Recall@K, MRR, and nDCG@K
- average and P95 retrieval latency
- empty-recall count and retrieval/rerank/fallback modes

Set optional environment variables:

```bash
export WATCHOPS_API_BASE_URL=http://localhost:8080
export WATCHOPS_RETRIEVAL_EVAL_CASES=testdata/retrieval_eval_cases.json
export WATCHOPS_RETRIEVAL_EVAL_TOP_K=5
export WATCHOPS_RETRIEVAL_EVAL_OUTPUT=tmp/retrieval_eval_report.json
```

Generated reports under `tmp/` are local artifacts and should not be committed.

## Intent and retrieval evaluation boundaries

Intent classification and direct natural-language slot extraction use a
separate offline dataset:

```bash
make eval-intent
```

This loads `testdata/intent_eval_cases.json` and reports Intent Accuracy, Slot
Field Accuracy for service/time range/Trace ID/symptom, and Joint Intent + Slot
Exact Match. It does not evaluate Clarify/Proceed decisions, Session Focus,
Graph routing, fallback contracts, or retrieval ranking. Those deterministic
governance behaviors are covered by `go test ./...`.

`make eval-retrieval` is the single retrieval-effectiveness evaluation path: it
calls the running WatchOps Knowledge API and Elasticsearch index. With
embeddings disabled it validates BM25; with embeddings enabled it can validate
Hybrid Retrieval. Local results are not equivalent to production retrieval
quality.

Thresholds are opt-in:

```bash
export WATCHOPS_INTENT_ACCURACY_MIN=0.80
export WATCHOPS_SLOT_EXTRACTION_ACCURACY_MIN=0.80
export WATCHOPS_JOINT_INTENT_SLOT_MIN=0.70
make eval-intent
```

Without these variables the command reports the real baseline and does not
invent a target. Eval failures remain review artifacts; they are not
automatically promoted to Good/Bad Cases or used to change production routing.

## Empty recall behavior

If retrieval returns no results, WatchOps-Lite should not invent knowledge evidence. Chat responses should include limitations when no knowledge evidence supports a claim. Other evidence tools, such as metrics, logs, traces, alerts, or topology, may still provide useful evidence.

The real retrieval eval runner marks empty retrieval as `empty_recall=true` so it is visible during demos and interviews.

## Evidence constraints reduce hallucination

The Agent can only support conclusions and inferences with evidence IDs that came from executed tools. If `search_knowledge` returns nothing, there are no knowledge evidence IDs to cite. This makes missing retrieval explicit instead of allowing the model to fabricate a runbook.

## Current limitations

- The default eval runner checks top-k keyword and ID hits; it is not an LLM judge.
- It evaluates the configured local retrieval mode rather than benchmarking every possible mode automatically.
- The rule-based scorer is intentionally simple and is not a learned cross-encoder.
- External provider quality and latency depend on the configured model; the eval never invents external results.
- The demo corpus is intentionally small, so some cases are useful as partial-recall or empty-recall probes.

Future improvements can add larger versioned datasets, before/after ranking comparison, corpus coverage reports, and CI-friendly container-backed retrieval tests.
