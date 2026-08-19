# WatchOps-Lite Evaluation Summary

- Dataset: `2026-08-18.v1`
- Evaluation mode: `offline_fixture`
- External LLM calls: `false`

## Governance Quality

### Intent Challenge

- Cases: 40
- Intent accuracy: 1.0000
- Slot completeness: 1.0000
- Clarification precision / recall: 1.0000 / 1.0000
- Over / under clarification: 0.0000 / 0.0000
- LLM escalation / incorrect escalation: 0.0500 / 0.0000

### Final Optimization Before / After

| Intent metric | Before | After |
|---|---:|---:|
| LLM escalation rate | 0.2250 | 0.0500 |
| Incorrect escalation rate | 0.1500 | 0.0000 |
| Challenge behavior accuracy | 1.0000 | 1.0000 |
| Fixed regression accuracy | 1.0000 | 1.0000 |

### Rule-first Hybrid Intent

| Mode | Rules direct | LLM escalations | LLM call rate | Fallbacks | Accuracy |
|---|---:|---:|---:|---:|---:|
| llm | 0 | 32 | 1.0000 | 0 | 1.0000 |
| hybrid | 30 | 2 | 0.0625 | 0 | 1.0000 |

### Clarification Early Exit

- Cases: 3
- RAG / Agent / Tool calls: 0 / 0 / 0
- Avoided RAG / Agent / Tool calls: 3 / 3 / 3

## Tool Semantics

- Cases: 17
- Required tool recall: 1.0000
- Forbidden tool violation rate: 0.0000
- Unnecessary tool call rate: 0.0000
- Tool path validity: 1.0000
- Legacy exact selection accuracy (compatibility): 0.7059
- Argument accuracy: 1.0000
- Argument `query_semantic`: 1.0000
- Argument `service`: 1.0000
- Argument `symptom`: 1.0000
- Argument `time_range`: 1.0000
- Argument `trace_id`: 1.0000

## Evidence Safety

- Citation validity: 1.0000
- Required evidence coverage: 1.0000
- Evidence allowlist violations: 0 (rate 0.0000)

## Agent E2E

- Cases: 17
- Task success: 1.0000
- Decision accuracy: 1.0000
- Required evidence coverage: 1.0000
- Unsafe action rate: 0.0000
- Completed / clarified / failed / timeout: 11 / 2 / 0 / 1
- Repeated tool call stops: 1
- Average tool calls: 1.3529
- Average agent steps: N/A
- P50 / P95 latency: 0.148 / 0.864 ms

### Agent E2E Before / After

| Metric | Before | After |
|---|---:|---:|
| Task success | 1.0000 | 1.0000 |
| Decision accuracy | 1.0000 | 1.0000 |
| Unsafe action rate | 0.0000 | 0.0000 |
| Timeout behavior count | 1 | 1 |
| Repeated tool stops | 1 | 1 |

## Retrieval Quality

| Mode | Query | Recall@1 | Recall@3 | Recall@5 | MRR | Candidates | Latency ms |
|---|---|---:|---:|---:|---:|---:|---:|
| bm25 | single_query | 0.5000 | 0.8571 | 1.0000 | 0.6750 | 6.43 | 0.026 |
| bm25 | multi_query | 0.5000 | 0.7857 | 1.0000 | 0.6810 | 6.43 | 0.055 |
| hybrid | single_query | 0.5714 | 0.9286 | 1.0000 | 0.7405 | 6.43 | 0.034 |
| hybrid | multi_query | 0.5000 | 0.8571 | 1.0000 | 0.6714 | 6.43 | 0.096 |
| hybrid+rerank | multi_query | 0.6429 | 0.8571 | 1.0000 | 0.7548 | 6.43 | 0.106 |
| hybrid+rerank | conditional | 0.6429 | 0.9286 | 1.0000 | 0.7762 | 6.43 | 0.042 |

### Conditional Multi-Query Decision

- Single / Multi cases: 13 / 1
- Trigger rate: 0.0714
- Correct / incorrect / neutral trigger rate: 1.0000 / 0.0000 / 0.0000
- Single-query subset Recall@5 / MRR: 1.0000 / 0.7590
- Multi-query subset Recall@5 / MRR: 1.0000 / 1.0000
- Conditional overall Recall@5 / MRR: 1.0000 / 0.7762

### Retrieval Before / After

| Metric | Before | After conditional subset |
|---|---:|---:|
| Single Query Recall@5 | 0.9286 | 1.0000 |
| Single Query MRR | 0.5583 | 0.7590 |
| Multi Query Recall@5 | 0.8571 | 1.0000 |
| Multi Query MRR | 0.5762 | 1.0000 |
| Retrieval bad cases | 2 | 0 |

## Context Governance

| Mode | Messages | Characters | Bytes | Focus bytes | Retained fields | Redacted fields |
|---|---:|---:|---:|---:|---|---|
| raw_history | 18 | 1058 | 1058 | 0 | service, time_range, trace_id |  |
| recent+summary+focus | 6 | 930 | 930 | 1640 | service, time_range, trace_id | api_key |

## Bad Case Before / After

- Before: 9 records / 3 unique cases
- After: 0 records / 0 unique cases
- Fixed / still failing / new regression: 9 / 0 / 0
- Challenge findings: 0

| Case | Stage | Root cause | Fix type | Before | After | Fixed |
|---|---|---|---|---|---|---:|
| payment-dependency-latency | clarification | cross-service dependency wording was treated as ambiguous multi-service input | rule_incomplete | clarified instead of proceeding with checkout as the primary service | passed behavior-level evaluation | true |
| database-connection-errors | intent | Chinese log and database connection error signals were absent from incident-over-knowledge precedence | code_bug | classified as knowledge_query instead of incident_triage | passed behavior-level evaluation | true |
| payment-dependency-latency | intent | compound trace plus Chinese latency plus runbook wording was not recognized as a composite investigation | rule_incomplete | classified as trace_analysis instead of incident_triage | passed behavior-level evaluation | true |
| database-connection-errors | slot | generic Chinese database connection error was not normalized to symptom=error | rule_incomplete | symptom was empty | passed behavior-level evaluation | true |
| database-connection-errors | tool_arguments | the planner selected only knowledge, so no service-aware log arguments were emitted | code_bug | no selected tool carried service=orders or the requested time range | passed behavior-level evaluation | true |
| payment-dependency-latency | tool_arguments | premature clarification prevented all tool argument generation | code_bug | no tool arguments were produced | passed behavior-level evaluation | true |
| checkout-error-rate-spike | tool_selection | exact-set evaluation rejected a reasonable metrics plus logs investigation path | eval_ground_truth_too_strict | query_logs was treated as incorrect even though it was relevant corroboration | passed behavior-level evaluation | true |
| database-connection-errors | tool_selection | Chinese explicit log signal was not recognized by the deterministic planner | code_bug | selected search_knowledge but omitted required query_logs | passed behavior-level evaluation | true |
| payment-dependency-latency | tool_selection | premature clarification stopped the tool path before trace and knowledge calls | code_bug | selected no tools | passed behavior-level evaluation | true |

## Final Optimization Bad Cases

- Before / After / Fixed: 8 / 0 / 8

| Case | Stage | Input | Root cause | Fix type | After | Fixed |
|---|---|---|---|---|---|---:|
| chat-greeting | agent_execution | 你好，今天过得怎么样 | clear non-operational requests shared the generic 0.50 confidence and fell below the hybrid threshold | rule_confidence | passed current evaluation | true |
| chat-rag-concept | agent_execution | Can you explain what RAG means? | explicit conceptual questions were not separated from ambiguous general chat | rule_confidence | passed current evaluation | true |
| irrelevant-chat | agent_execution | 写一首关于墨尔本下雨的俳句 | explicit creative requests were not recognized as strong deterministic general intent | rule_confidence | passed current evaluation | true |
| runbook-primary | agent_execution | payment latency runbook，纯粹找文档 | latency plus runbook terms were treated as conflict despite the explicit documentation-only preference | conflict_detection | passed current evaluation | true |
| switch-to-runbook | agent_execution | 不用查指标了，找一下 checkout runbook | the resolved knowledge preference was classified correctly but conflict metadata still forced escalation | conflict_detection | passed current evaluation | true |
| too-many-connections | agent_execution | inventory 报 too many connections，查日志和 runbook | multiple complementary evidence terms were mistaken for mutually exclusive intent signals | conflict_detection | passed current evaluation | true |
| correct-previous-parameters | retrieval | 改成 checkout，时间用最近 5 分钟 | the correction turn lacked an authoritative semantic query and generic expansion diluted parameter-correction evidence | authoritative_query | passed current evaluation | true |
| switch-service-correction | retrieval | 别查 checkout 了，改查 payment 最近 5 分钟错误日志 | obsolete service wording remained in the original query while unconditional expansion favored generic observability documents | authoritative_query | passed current evaluation | true |

## Open Quality Findings

- Total: 0
- None

## Fixed Regression Safety Suite

- Intent cases: 32
- Intent accuracy: 1.0000
- Slot accuracy: 1.0000
- Joint exact match: 1.0000
- Clarification accuracy: 1.0000
