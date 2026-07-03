"use strict";

const state = {
  latestChatResponse: null,
  latestRequestId: "",
  latestTraceId: "",
  latestSessionId: "",
  latestHistorySessionId: "",
  latestSSEEvents: [],
  lastLatencyMS: null,
  selectedRating: "",
  latestHistoryResponse: null,
  latestKnowledgeResults: null,
};

const sourceLabels = {
  metrics: "source.metrics",
  logs: "source.logs",
  traces: "source.traces",
  knowledge: "source.knowledge",
  alerts: "source.alerts",
  topology: "source.topology",
  memory: "source.memory",
  long_term_memory: "source.memory",
  unknown: "source.unknown",
};

const presets = {
  zh: {
    error_rate: "过去 20 分钟 checkout 服务错误率为什么升高？",
    payment_timeout: "过去 20 分钟 payment 超时是否导致 checkout 失败？",
    slow_trace: "分析过去 20 分钟 checkout 慢 Trace，并定位瓶颈。",
    runbook: "查找 checkout 事故 runbook，并总结缓解步骤。",
    alerts: "检查过去 20 分钟 checkout 的活跃告警。",
    topology: "展示 checkout 服务拓扑和相关依赖。",
  },
  en: {
    error_rate: "Why is the checkout error rate high in the last 20 minutes?",
    payment_timeout: "Are payment timeouts causing checkout failures in the last 20 minutes?",
    slow_trace: "Analyze slow checkout traces from the last 20 minutes and identify the bottleneck.",
    runbook: "Find the checkout incident runbook and summarize the mitigation steps.",
    alerts: "Check active checkout alerts for the last 20 minutes.",
    topology: "Show the checkout service topology and relevant dependencies.",
  },
};

const byId = (id) => document.getElementById(id);
const t = (key, replacements = {}) => window.WatchOpsI18n?.t(key, replacements) || key;
const currentLanguage = () => window.WatchOpsI18n?.getLanguage() || "zh";

document.addEventListener("DOMContentLoaded", () => {
  bindNavigation();
  bindActions();
  updateRuntimeContext();
  window.addEventListener("watchops:languagechange", rerenderLocalizedState);
});

function bindNavigation() {
  document.querySelectorAll(".nav-item").forEach((button) => {
    button.addEventListener("click", () => activateTab(button.dataset.tab));
  });
}

function activateTab(name) {
  document.querySelectorAll(".nav-item").forEach((button) => {
    button.classList.toggle("active", button.dataset.tab === name);
  });
  document.querySelectorAll(".tab-panel").forEach((panel) => {
    panel.classList.toggle("active", panel.id === `tab-${name}`);
  });
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function bindActions() {
  document.querySelectorAll(".query-chip").forEach((button) => {
    button.addEventListener("click", () => {
      const languagePresets = presets[currentLanguage()] || presets.zh;
      byId("message").value = languagePresets[button.dataset.preset] || button.textContent.trim();
      byId("message").focus();
    });
  });

  byId("send-chat").addEventListener("click", sendChat);
  byId("send-stream").addEventListener("click", () => {
    activateTab("stream");
    sendStreamChat();
  });
  byId("start-stream").addEventListener("click", sendStreamChat);
  byId("search-knowledge").addEventListener("click", searchKnowledge);
  byId("load-history").addEventListener("click", () => loadHistory());
  byId("clear-history").addEventListener("click", clearHistory);
  byId("knowledge-query").addEventListener("keydown", (event) => {
    if (event.key === "Enter") searchKnowledge();
  });

  document.querySelectorAll(".rating-button").forEach((button) => {
    button.addEventListener("click", () => selectRating(button.dataset.rating));
  });
  byId("submit-feedback").addEventListener("click", submitFeedback);
  byId("run-eval").addEventListener("click", runEval);
  document.querySelectorAll("[data-copy]").forEach((button) => {
    button.addEventListener("click", () => copyToClipboard(button.dataset.copy));
  });
}

function rerenderLocalizedState() {
  const question = byId("message").value.trim();
  const knownDefaultQuestions = Object.values(presets).map((values) => values.error_rate);
  if (knownDefaultQuestions.includes(question)) {
    byId("message").value = presets[currentLanguage()].error_rate;
  }
  const knowledgeQuery = byId("knowledge-query").value.trim();
  if (["checkout timeout runbook", "checkout 支付超时排障 runbook"].includes(knowledgeQuery)) {
    byId("knowledge-query").value = currentLanguage() === "zh" ?
      "checkout 支付超时排障 runbook" : "checkout timeout runbook";
  }
  if (state.latestChatResponse) {
    renderChatResponse(state.latestChatResponse);
    renderToolRuns(state.latestChatResponse.tool_runs);
    renderEvidenceGroups(state.latestChatResponse?.answer?.evidence);
    renderMemoryContext(state.latestChatResponse);
    setResponseStatus(t("common.completed"), "success");
  }
  if (state.latestKnowledgeResults !== null) {
    renderKnowledgeResults(state.latestKnowledgeResults);
  }
  if (state.latestHistoryResponse) {
    renderHistory(state.latestHistoryResponse);
  }
  if (state.latestSSEEvents.length) {
    byId("stream-timeline").innerHTML = "";
    state.latestSSEEvents.forEach((event) => renderSSEEvent(event.type, event.data));
  }
  updateFeedbackAvailability();
  updateRuntimeContext();
}

async function apiFetch(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {
      Accept: "application/json",
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...(options.headers || {}),
    },
  });
  const text = await response.text();
  let payload = {};
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      throw new Error(t("dynamic.invalid_json", { status: response.status }));
    }
  }
  if (!response.ok) {
    const message = payload?.error?.message || payload?.message ||
      t("dynamic.request_failed", { status: response.status });
    throw new Error(message);
  }
  return payload;
}

function buildChatPayload(streamSuffix = "") {
  const sessionID = byId("session-id").value.trim();
  const message = byId("message").value.trim();
  if (!sessionID || !message) {
    throw new Error(t("dynamic.chat_required"));
  }
  const now = new Date();
  const from = new Date(now.getTime() - 20 * 60 * 1000);
  return {
    session_id: streamSuffix ? `${sessionID}${streamSuffix}` : sessionID,
    message,
    time_context: {
      from: from.toISOString(),
      to: now.toISOString(),
    },
  };
}

async function sendChat() {
  const button = byId("send-chat");
  try {
    const payload = buildChatPayload();
    setLoading(button, true, t("common.sending"));
    setResponseStatus(t("common.running"), "info");
    const started = performance.now();
    const response = await apiFetch("/api/v1/chat", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    state.lastLatencyMS = performance.now() - started;
    acceptChatResponse(response);
    setResponseStatus(t("common.completed"), "success");
    renderToast(t("dynamic.chat_completed"), "success");
  } catch (error) {
    setResponseStatus(t("common.failed"), "danger");
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, t("chat.send"));
  }
}

async function sendStreamChat() {
  const buttons = [byId("send-stream"), byId("start-stream")];
  try {
    const payload = buildChatPayload("-stream");
    buttons.forEach((button) => setLoading(button, true, t("common.streaming")));
    state.latestSSEEvents = [];
    byId("stream-timeline").innerHTML = "";
    setResponseStatus(t("common.streaming"), "info");
    const started = performance.now();
    const response = await fetch("/api/v1/chat/stream", {
      method: "POST",
      headers: {
        Accept: "text/event-stream",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
    });
    if (!response.ok || !response.body) {
      const text = await response.text();
      let message = t("dynamic.stream_http_failed", { status: response.status });
      try {
        message = JSON.parse(text)?.error?.message || message;
      } catch {
        // Keep the bounded HTTP error; never render arbitrary response HTML.
      }
      throw new Error(message);
    }
    const finalAnswerReceived = await consumeSSE(response.body);
    state.lastLatencyMS = performance.now() - started;
    if (!finalAnswerReceived) {
      throw new Error(t("dynamic.stream_no_answer"));
    }
    setResponseStatus(t("common.completed"), "success");
    updateRuntimeContext();
    renderToast(t("dynamic.stream_completed"), "success");
  } catch (error) {
    setResponseStatus(t("common.failed"), "danger");
    renderSSEEvent("workflow_failed", {
      timestamp: new Date().toISOString(),
      message: error.message,
    });
    renderToast(error.message, "error");
  } finally {
    setLoading(byId("send-stream"), false, t("chat.stream"));
    setLoading(byId("start-stream"), false, t("stream.start"));
  }
}

async function consumeSSE(stream) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let finalAnswerReceived = false;
  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done });
    buffer = buffer.replace(/\r\n/g, "\n");
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      if (processSSEBlock(block) === "final_answer") finalAnswerReceived = true;
      boundary = buffer.indexOf("\n\n");
    }
    if (done) break;
  }
  if (buffer.trim() && processSSEBlock(buffer) === "final_answer") finalAnswerReceived = true;
  return finalAnswerReceived;
}

function processSSEBlock(block) {
  let eventType = "message";
  const dataLines = [];
  block.split("\n").forEach((line) => {
    if (line.startsWith("event:")) eventType = line.slice(6).trim();
    if (line.startsWith("data:")) dataLines.push(line.slice(5).trimStart());
  });
  if (!dataLines.length) return eventType;
  let data = {};
  try {
    data = JSON.parse(dataLines.join("\n"));
  } catch {
    data = { message: t("dynamic.event_decode_failed") };
  }
  state.latestSSEEvents.push({ type: eventType, data });
  renderSSEEvent(eventType, data);
  if (eventType === "final_answer" && data && typeof data === "object") {
    acceptChatResponse(data);
  }
  return eventType;
}

function renderSSEEvent(type, data = {}) {
  const timeline = byId("stream-timeline");
  const tone = eventTone(type);
  const event = document.createElement("article");
  event.className = `timeline-event ${tone}${type === "final_answer" ? " final" : ""}`;
  const safeMessage = describeSSEEvent(type, data);
  const timestamp = formatTimestamp(data.timestamp);
  event.innerHTML = `
    <div class="event-card">
      <div class="event-topline">
        <span class="badge ${tone === "success" ? "success" : tone === "danger" ? "danger" : tone === "warning" ? "warning" : "info"}">${escapeHtml(type)}</span>
        <time class="event-time">${escapeHtml(timestamp)}</time>
      </div>
      <p class="event-message">${escapeHtml(safeMessage)}</p>
    </div>`;
  timeline.appendChild(event);
  event.scrollIntoView({ block: "nearest", behavior: "smooth" });
}

function describeSSEEvent(type, data) {
  const tool = safeText(data.tool || data.tool_name);
  const node = safeText(data.node || data.node_name);
  const evidenceCount = safeNumber(data.evidence_count);
  const latency = safeNumber(data.latency_ms);
  const errorCode = safeText(data.error_code || data.code);
  const availability = data.available === false ? t("event.limited_availability") : "";
  const latencyText = latency === null ? "" : t("event.tool_latency", {
    latency: formatLatency(latency),
  });
  const evidenceText = evidenceCount === null ? "" : t("event.tool_evidence", {
    count: evidenceCount,
  });
  const codeText = errorCode ? t("event.with_code", { code: errorCode }) : "";
  const messageText = data.message ? t("event.message_suffix", {
    message: safeText(data.message),
  }) : "";
  const messages = {
    workflow_started: t("event.workflow_started", {
      session: safeText(data.session_id) || t("context.session"),
    }),
    graph_node_started: t("event.node_started", { node: node || t("common.unknown") }),
    graph_node_completed: t("event.node_completed", { node: node || t("common.unknown") }),
    memory_loaded: t("event.memory_loaded", { availability }),
    tool_call_started: t("event.tool_started", { tool: tool || t("common.unknown") }),
    tool_call_completed: t("event.tool_completed", {
      tool: tool || t("common.unknown"),
      latency: latencyText,
      evidence: evidenceText,
    }),
    tool_call_failed: t("event.tool_failed", {
      tool: tool || t("common.unknown"),
      code: codeText,
    }),
    evidence_collected: t("event.evidence_collected", { count: evidenceCount ?? 0 }),
    failure_controller_triggered: t("event.failure_controller", { code: codeText }),
    final_answer: t("event.final_answer"),
    workflow_completed: t("event.workflow_completed"),
    workflow_failed: t("event.workflow_failed", {
      code: codeText,
      message: messageText,
    }),
  };
  return messages[type] || t("event.operational", { type });
}

function eventTone(type) {
  if (type.includes("failed")) return "danger";
  if (type.includes("failure_controller")) return "warning";
  if (type.includes("completed") || type === "final_answer") return "success";
  return "info";
}

function acceptChatResponse(response) {
  state.latestChatResponse = response || {};
  state.latestRequestId = safeText(response?.request_id);
  state.latestTraceId = safeText(response?.trace_id);
  state.latestSessionId = safeText(response?.session_id);
  renderChatResponse(response);
  renderToolRuns(response?.tool_runs);
  renderEvidenceGroups(response?.answer?.evidence);
  renderMemoryContext(response);
  updateRuntimeContext();
  updateFeedbackAvailability();
  if (state.latestSessionId) {
    void loadHistory(state.latestSessionId, { quiet: true });
  }
}

function renderChatResponse(response) {
  const answer = response?.answer || {};
  const evidence = safeArray(answer.evidence);
  const toolRuns = safeArray(response?.tool_runs);
  const inferences = safeArray(answer.inferences);
  const limitations = safeArray(answer.limitations);
  byId("response-summary").innerHTML = metricCards([
    [t("common.evidence"), evidence.length],
    [t("common.tool_runs"), toolRuns.length],
    [t("common.inferences"), inferences.length],
    [t("common.limitations"), limitations.length],
  ]);

  const sections = [
    renderStatementSection(t("common.conclusions"), answer.conclusion, t("dynamic.no_conclusions")),
    renderCompactEvidence(evidence),
    renderStatementSection(t("common.inferences"), inferences, t("dynamic.no_inferences")),
    renderStatementSection(t("common.recommendations"), answer.recommendations, t("dynamic.no_recommendations")),
    renderLimitationSection(limitations),
    renderMetadata(response?.metadata),
  ];
  byId("chat-response").className = "";
  byId("chat-response").innerHTML = sections.join("");
}

function renderStatementSection(title, items, emptyMessage) {
  const values = safeArray(items);
  const content = values.length
    ? `<ul class="response-list">${values.map((item) => `
        <li>
          ${escapeHtml(item?.text || String(item || ""))}
          ${safeArray(item?.evidence_ids).length ? `<span class="evidence-refs">${escapeHtml(t("common.evidence"))}: ${escapeHtml(item.evidence_ids.join(", "))}</span>` : ""}
        </li>`).join("")}</ul>`
    : `<p class="subtle">${escapeHtml(emptyMessage)}</p>`;
  return `<section class="response-section"><h4>${escapeHtml(title)}</h4>${content}</section>`;
}

function renderCompactEvidence(items) {
  const content = items.length
    ? `<ul class="response-list">${items.slice(0, 6).map((item) => `
        <li>
          <span class="source-badge">${escapeHtml(sourceLabel(item?.source_type))}</span>
          ${escapeHtml(item?.content || t("dynamic.evidence_unavailable"))}
          <span class="evidence-refs">${escapeHtml(item?.id || t("dynamic.no_evidence_id"))}</span>
        </li>`).join("")}</ul>`
    : `<p class="subtle">${escapeHtml(t("dynamic.no_evidence"))}</p>`;
  return `<section class="response-section"><h4>${escapeHtml(t("common.evidence"))}</h4>${content}</section>`;
}

function renderLimitationSection(items) {
  const content = items.length
    ? `<ul class="response-list">${items.map((item) => `
        <li class="warning-row">
          <strong>${escapeHtml(item?.code || "LIMITATION")}</strong> · ${escapeHtml(item?.message || t("dynamic.no_detail"))}
          ${item?.tool ? `<span class="evidence-refs">${escapeHtml(t("dynamic.tool"))}: ${escapeHtml(item.tool)}</span>` : ""}
        </li>`).join("")}</ul>`
    : `<p class="subtle">${escapeHtml(t("dynamic.no_limitations"))}</p>`;
  return `<section class="response-section"><h4>${escapeHtml(t("common.limitations"))}</h4>${content}</section>`;
}

function renderMetadata(metadata) {
  return `<section class="response-section">
    <h4>${escapeHtml(t("common.metadata"))}</h4>
    <details>
      <summary>${escapeHtml(t("dynamic.view_metadata"))}</summary>
      <pre>${escapeHtml(safeJson(metadata || {}))}</pre>
    </details>
  </section>`;
}

function renderToolRuns(toolRuns) {
  const runs = safeArray(toolRuns);
  if (!runs.length) {
    byId("tool-runs").innerHTML = `<div class="empty-state compact"><p>${escapeHtml(t("evidence.no_tools"))}</p></div>`;
  } else {
    byId("tool-runs").innerHTML = `
      <table>
        <thead><tr><th>${escapeHtml(t("dynamic.tool"))}</th><th>${escapeHtml(t("common.status"))}</th><th>${escapeHtml(t("dynamic.latency"))}</th><th>${escapeHtml(t("common.evidence"))}</th><th>${escapeHtml(t("dynamic.fallback_error"))}</th></tr></thead>
        <tbody>${runs.map((run) => {
          const failed = run?.success === false;
          const fallback = Number(run?.warning_count || 0) > 0;
          const status = failed ? t("common.failed") : fallback ? t("stream.fallback") : t("dynamic.success");
          const tone = failed ? "danger" : fallback ? "warning" : "success";
          const detail = run?.error_code || (fallback ? t("dynamic.warning_count", {
            count: run.warning_count,
          }) : "—");
          return `<tr>
            <td><strong>${escapeHtml(run?.tool || t("common.unknown"))}</strong></td>
            <td><span class="badge ${tone}">${status}</span></td>
            <td>${escapeHtml(formatLatency(run?.duration_ms))}</td>
            <td>${escapeHtml(String(run?.evidence_count ?? 0))}</td>
            <td>${escapeHtml(detail)}</td>
          </tr>`;
        }).join("")}</tbody>
      </table>`;
  }
  renderEvidenceSummary();
}

function renderEvidenceGroups(evidence) {
  const items = safeArray(evidence);
  const groups = groupBySourceType(items);
  const container = byId("evidence-groups");
  if (!items.length) {
    container.innerHTML = `<div class="card empty-state"><h4>${escapeHtml(t("evidence.empty_title"))}</h4><p>${escapeHtml(t("dynamic.agent_no_evidence"))}</p></div>`;
    renderEvidenceSummary();
    return;
  }
  container.innerHTML = Object.entries(groups).map(([source, values]) => `
    <article class="card">
      <div class="card-heading">
        <h3>${escapeHtml(sourceLabel(source))}</h3>
        <span class="source-badge">${escapeHtml(t("dynamic.item_count", { count: values.length }))}</span>
      </div>
      ${values.map((item) => `
        <div class="evidence-row">
          <p>${escapeHtml(truncateText(item?.content || t("dynamic.evidence_unavailable"), 520))}</p>
          <div class="evidence-meta">
            <span>${escapeHtml(item?.id || "no-id")}</span>
            ${item?.source_name ? `<span>${escapeHtml(item.source_name)}</span>` : ""}
            ${item?.score !== undefined ? `<span>score ${escapeHtml(String(item.score))}</span>` : ""}
          </div>
          ${item?.metadata ? `<details><summary>${escapeHtml(t("dynamic.evidence_metadata"))}</summary><pre>${escapeHtml(safeJson(item.metadata))}</pre></details>` : ""}
        </div>`).join("")}
    </article>`).join("");
  renderEvidenceSummary();
}

function renderEvidenceSummary() {
  const response = state.latestChatResponse || {};
  const runs = safeArray(response?.tool_runs);
  const evidence = safeArray(response?.answer?.evidence);
  const limitations = safeArray(response?.answer?.limitations);
  const successful = runs.filter((run) => run?.success !== false).length;
  const trace = safeText(response?.trace_id);
  byId("evidence-summary").innerHTML = metricCards([
    [t("common.tool_runs"), runs.length],
    [t("evidence.successful"), successful],
    [t("common.failed"), runs.length - successful],
    [t("common.evidence"), evidence.length],
    [t("common.limitations"), limitations.length],
    [t("context.trace"), trace ? shortID(trace) : "—"],
  ]);
}

function groupBySourceType(items) {
  return safeArray(items).reduce((groups, item) => {
    const source = safeText(item?.source_type).toLowerCase() || "unknown";
    (groups[source] ||= []).push(item);
    return groups;
  }, {});
}

async function searchKnowledge() {
  const button = byId("search-knowledge");
  const query = byId("knowledge-query").value.trim();
  if (!query) {
    renderToast(t("dynamic.enter_knowledge"), "error");
    return;
  }
  try {
    setLoading(button, true, t("common.searching"));
    const response = await apiFetch("/api/v1/knowledge/search", {
      method: "POST",
      body: JSON.stringify({ query, limit: 5, filters: {} }),
    });
    renderKnowledgeResults(response?.results);
    renderToast(t("dynamic.knowledge_completed"), "success");
  } catch (error) {
    byId("knowledge-results").innerHTML = polishedEmpty(error.message);
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, t("knowledge.search"));
  }
}

function renderKnowledgeResults(results) {
  const items = safeArray(results);
  state.latestKnowledgeResults = items;
  if (!items.length) {
    byId("knowledge-results").innerHTML = polishedEmpty(t("dynamic.no_knowledge"));
    return;
  }
  byId("knowledge-results").innerHTML = items.map((item) => `
    <article class="search-result">
      <h4>${escapeHtml(item?.title || t("dynamic.untitled_knowledge"))}</h4>
      <p>${escapeHtml(truncateText(item?.content || t("dynamic.no_snippet"), 420))}</p>
      <div class="result-meta">
        <span>document ${escapeHtml(item?.document_id || "—")}</span>
        <span>chunk ${escapeHtml(item?.chunk_id || "—")}</span>
        <span>score ${escapeHtml(String(item?.score ?? "—"))}</span>
        ${item?.metadata?.retrieval_mode ? `<span>${escapeHtml(item.metadata.retrieval_mode)}</span>` : ""}
      </div>
      ${item?.metadata ? `<details><summary>${escapeHtml(t("dynamic.retrieval_metadata"))}</summary><pre>${escapeHtml(safeJson(item.metadata))}</pre></details>` : ""}
    </article>`).join("");
}

function renderMemoryContext(response) {
  const metadata = response?.metadata || {};
  const evidence = safeArray(response?.answer?.evidence);
  const memoryEvidence = evidence.filter((item) => {
    const type = safeText(item?.source_type).toLowerCase();
    return type === "memory" || type === "long_term_memory";
  });
  const sessionAvailable = metadata.session_memory_available;
  const longTermAvailable = metadata.long_term_memory_available;
  byId("memory-context").className = "";
  byId("memory-context").innerHTML = `
    <div class="memory-status-grid">
      <article><span>${escapeHtml(t("dynamic.session_memory"))}</span><strong>${availabilityLabel(sessionAvailable)}</strong></article>
      <article><span>${escapeHtml(t("dynamic.long_term_memory"))}</span><strong>${availabilityLabel(longTermAvailable)}</strong></article>
    </div>
    ${memoryEvidence.length
      ? memoryEvidence.map((item) => `<div class="evidence-row"><p>${escapeHtml(item?.content || t("dynamic.memory_unavailable"))}</p><div class="evidence-meta"><span>${escapeHtml(item?.id || "memory")}</span></div></div>`).join("")
      : `<div class="inline-note">${escapeHtml(t("dynamic.no_memory_evidence"))}</div>`}
    <details><summary>${escapeHtml(t("dynamic.visible_chat_metadata"))}</summary><pre>${escapeHtml(safeJson(metadata))}</pre></details>`;
}

async function loadHistory(sessionID = "", options = {}) {
  const button = byId("load-history");
  const selectedSessionID = safeText(sessionID) || byId("session-id").value.trim();
  if (!selectedSessionID) {
    if (!options.quiet) renderToast(t("dynamic.session_load_required"), "error");
    return;
  }
  try {
    if (!options.quiet) setLoading(button, true, t("common.loading"));
    const response = await apiFetch(
      `/api/v1/chat/history?session_id=${encodeURIComponent(selectedSessionID)}&limit=20`,
    );
    renderHistory(response);
    if (!options.quiet) renderToast(t("dynamic.history_loaded"), "success");
  } catch (error) {
    renderHistoryError(error.message);
    if (!options.quiet) renderToast(error.message, "error");
  } finally {
    if (!options.quiet) setLoading(button, false, t("history.load"));
  }
}

async function clearHistory() {
  const button = byId("clear-history");
  const sessionID = state.latestHistorySessionId || byId("session-id").value.trim();
  if (!sessionID) {
    renderToast(t("dynamic.session_clear_required"), "error");
    return;
  }
  if (!window.confirm(t("dynamic.clear_confirm", { session: sessionID }))) {
    return;
  }
  try {
    setLoading(button, true, t("common.clearing"));
    await apiFetch(
      `/api/v1/chat/history?session_id=${encodeURIComponent(sessionID)}`,
      { method: "DELETE" },
    );
    renderHistory({
      session_id: sessionID,
      summary: {},
      messages: [],
      count: 0,
      limit: 20,
    });
    renderToast(t("dynamic.history_cleared"), "success");
  } catch (error) {
    renderHistoryError(error.message);
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, t("history.clear"));
  }
}

function renderHistory(response) {
  state.latestHistoryResponse = response;
  state.latestHistorySessionId = safeText(response?.session_id);
  const summary = response?.summary || {};
  const messages = safeArray(response?.messages);
  const summarySignals = [
    [t("dynamic.goal"), summary.goal],
    [t("dynamic.confirmed_facts"), safeArray(summary.confirmed_facts).join(" · ")],
    [t("dynamic.open_questions"), safeArray(summary.open_questions).join(" · ")],
  ].filter(([, value]) => safeText(value));
  byId("history-summary").innerHTML = safeText(summary.content) || summarySignals.length
    ? `<div class="session-summary-card">
        <div><span>${escapeHtml(t("dynamic.rolling_summary"))}</span><strong>v${escapeHtml(String(summary.version ?? 0))}</strong></div>
        ${summary.content ? `<p>${escapeHtml(summary.content)}</p>` : ""}
        ${summarySignals.map(([label, value]) => `
          <p><b>${escapeHtml(label)}:</b> ${escapeHtml(value)}</p>`).join("")}
      </div>`
    : `<div class="empty-state compact"><p>${escapeHtml(t("dynamic.no_rolling_summary"))}</p></div>`;

  if (!messages.length) {
    byId("history-messages").innerHTML =
      `<div class="empty-state compact"><p>${escapeHtml(t("dynamic.no_recent_messages"))}</p></div>`;
    return;
  }
  byId("history-messages").innerHTML = messages.map((message) => {
    const role = safeText(message?.role).toLowerCase() || "unknown";
    const roleClass = role === "user" ? "user" : role === "assistant" ? "assistant" : "system";
    return `<article class="history-message ${roleClass}">
      <div class="history-message-head">
        <strong>${escapeHtml(role)}</strong>
        <time>${escapeHtml(formatHistoryTime(message?.created_at))}</time>
      </div>
      <p>${escapeHtml(message?.content || t("dynamic.message_unavailable"))}</p>
      ${message?.metadata ? `
        <details>
          <summary>${escapeHtml(t("dynamic.message_metadata"))}</summary>
          <pre>${escapeHtml(safeJson(message.metadata))}</pre>
        </details>` : ""}
    </article>`;
  }).join("");
}

function renderHistoryError(message) {
  byId("history-summary").innerHTML =
    `<div class="empty-state compact"><p>${escapeHtml(t("dynamic.summary_unavailable"))}</p></div>`;
  byId("history-messages").innerHTML =
    polishedEmpty(t("dynamic.history_unavailable", { message }));
}

function formatHistoryTime(value) {
  if (!value) return t("dynamic.time_unavailable");
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return t("dynamic.time_unavailable");
  return date.toLocaleString(currentLanguage() === "zh" ? "zh-CN" : "en");
}

function selectRating(rating) {
  state.selectedRating = rating;
  document.querySelectorAll(".rating-button").forEach((button) => {
    button.classList.toggle("active", button.dataset.rating === rating);
  });
  updateFeedbackAvailability();
}

function updateFeedbackAvailability() {
  const enabled = Boolean(state.latestChatResponse && state.selectedRating);
  byId("submit-feedback").disabled = !enabled;
  if (!state.latestChatResponse) {
    byId("feedback-result").textContent = t("feedback.requires_chat");
  } else if (!state.selectedRating) {
    byId("feedback-result").textContent = t("dynamic.choose_rating");
  }
}

async function submitFeedback() {
  if (!state.latestChatResponse || !state.selectedRating) return;
  const button = byId("submit-feedback");
  const response = state.latestChatResponse;
  const evidenceIDs = safeArray(response?.answer?.evidence)
    .map((item) => safeText(item?.id))
    .filter(Boolean);
  try {
    setLoading(button, true, t("common.submitting"));
    const result = await apiFetch("/api/v1/feedback", {
      method: "POST",
      body: JSON.stringify({
        request_id: state.latestRequestId,
        session_id: state.latestSessionId,
        rating: state.selectedRating,
        reason_tags: [],
        comment: byId("feedback-comment").value.trim(),
        corrected_answer: "",
        answer_snapshot: response?.answer || {},
        evidence_ids: evidenceIDs,
        tool_runs: safeArray(response?.tool_runs),
        metadata: { source: "local_demo_console" },
      }),
    });
    byId("feedback-result").textContent = t("dynamic.feedback_created", {
      id: result.feedback_id || "",
    });
    renderToast(t("dynamic.feedback_submitted"), "success");
  } catch (error) {
    byId("feedback-result").textContent = t("dynamic.feedback_unavailable", {
      message: error.message,
    });
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, t("feedback.submit"));
    updateFeedbackAvailability();
  }
}

async function runEval() {
  const button = byId("run-eval");
  const caseType = byId("eval-case-type").value;
  const limit = Math.max(1, Math.min(20, Number(byId("eval-limit").value) || 5));
  try {
    setLoading(button, true, t("common.running"));
    const result = await apiFetch("/api/v1/eval/runs", {
      method: "POST",
      body: JSON.stringify({ case_type: caseType, limit }),
    });
    byId("eval-result").className = "";
    byId("eval-result").innerHTML = `
      <div class="memory-status-grid">
        <article><span>${escapeHtml(t("common.total"))}</span><strong>${escapeHtml(String(result?.total ?? 0))}</strong></article>
        <article><span>${escapeHtml(t("common.passed"))}</span><strong>${escapeHtml(String(result?.passed ?? 0))}</strong></article>
        <article><span>${escapeHtml(t("common.failed"))}</span><strong>${escapeHtml(String(result?.failed ?? 0))}</strong></article>
        <article><span>${escapeHtml(t("common.status"))}</span><strong>${escapeHtml(result?.status || t("common.unknown"))}</strong></article>
      </div>
      <div class="inline-note">${escapeHtml(t("dynamic.run_id"))}: ${escapeHtml(result?.run_id || t("dynamic.not_returned"))}</div>`;
    renderToast(t("dynamic.eval_completed"), "success");
  } catch (error) {
    byId("eval-result").innerHTML = polishedEmpty(t("dynamic.eval_unavailable", {
      message: error.message,
    }));
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, t("eval.run"));
  }
}

function updateRuntimeContext() {
  const response = state.latestChatResponse;
  const hasResponse = Boolean(response);
  byId("runtime-empty").classList.toggle("hidden", hasResponse);
  byId("runtime-context").classList.toggle("hidden", !hasResponse);
  setCopyValue("sidebar-request-id", state.latestRequestId);
  setCopyValue("sidebar-trace-id", state.latestTraceId);
  if (!hasResponse) return;

  const evidence = safeArray(response?.answer?.evidence);
  const limitations = safeArray(response?.answer?.limitations);
  const runs = safeArray(response?.tool_runs);
  setText("insight-session", state.latestSessionId || "—");
  setText("insight-request", state.latestRequestId || "—");
  setText("insight-trace", state.latestTraceId || "—");
  setText("insight-evidence", String(evidence.length));
  setText("insight-tools", String(runs.length));
  setText("insight-limitations", String(limitations.length));
  setText("insight-latency", state.lastLatencyMS === null ? "—" : formatLatency(state.lastLatencyMS));

  const traceLink = byId("trace-link");
  if (state.latestTraceId) {
    traceLink.href = `http://localhost:16686/trace/${encodeURIComponent(state.latestTraceId)}`;
    traceLink.classList.remove("disabled");
  } else {
    traceLink.href = "http://localhost:16686";
    traceLink.classList.add("disabled");
  }

  const metadata = response?.metadata || {};
  byId("memory-pills").innerHTML = [
    memoryPill("Redis", metadata.session_memory_available),
    memoryPill("MySQL", metadata.long_term_memory_available),
  ].join("");
  byId("latest-tools").innerHTML = runs.length
    ? runs.slice(-5).map((run) => `<div class="mini-tool"><span>${escapeHtml(run?.tool || t("common.unknown"))}</span><span class="badge ${run?.success === false ? "danger" : "success"}">${run?.success === false ? escapeHtml(t("common.failed")) : "ok"}</span></div>`).join("")
    : `<span class="subtle">${escapeHtml(t("context.no_tools"))}</span>`;
}

function memoryPill(name, available) {
  const label = available === true ? t("dynamic.ready") :
    available === false ? t("dynamic.limited") : t("common.unknown");
  return `<span class="memory-pill ${available === true ? "available" : ""}">${escapeHtml(name)} · ${label}</span>`;
}

function setCopyValue(id, value) {
  const element = byId(id);
  element.textContent = value ? shortID(value) : "—";
  element.dataset.copy = value || "";
  element.title = value ? `Copy ${value}` : t("dynamic.no_id");
}

async function copyToClipboard(value) {
  if (!value) {
    renderToast(t("dynamic.no_id"), "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    renderToast(t("dynamic.copied"), "success");
  } catch {
    renderToast(t("dynamic.clipboard_unavailable"), "error");
  }
}

function setLoading(button, loading, label) {
  if (!button) return;
  button.disabled = loading;
  button.classList.toggle("loading", loading);
  const text = button.querySelector(".button-label");
  if (text) text.textContent = label;
}

function setResponseStatus(text, tone) {
  const status = byId("response-status");
  status.textContent = text;
  status.className = `badge ${tone}`;
}

function renderToast(message, tone = "") {
  const toast = byId("toast");
  toast.textContent = message;
  toast.className = `toast visible ${tone}`;
  clearTimeout(renderToast.timer);
  renderToast.timer = setTimeout(() => {
    toast.className = "toast";
  }, 3600);
}

function metricCards(items) {
  return items.map(([label, value]) => `<article><span>${escapeHtml(label)}</span><strong title="${escapeHtml(String(value))}">${escapeHtml(String(value))}</strong></article>`).join("");
}

function polishedEmpty(message) {
  return `<div class="empty-state compact"><p>${escapeHtml(message)}</p></div>`;
}

function sourceLabel(value) {
  const key = safeText(value).toLowerCase() || "unknown";
  return sourceLabels[key] ? t(sourceLabels[key]) :
    key.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function availabilityLabel(value) {
  if (value === true) return t("common.available");
  if (value === false) return t("common.degraded");
  return t("common.not_exposed");
}

function safeArray(value) {
  return Array.isArray(value) ? value : [];
}

function safeText(value) {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function safeNumber(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function safeJson(value) {
  try {
    return JSON.stringify(value ?? {}, null, 2);
  } catch {
    return JSON.stringify({ message: t("dynamic.metadata_serialize_failed") }, null, 2);
  }
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function formatLatency(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return "—";
  if (number < 1000) return `${number.toFixed(number < 10 ? 1 : 0)} ms`;
  return `${(number / 1000).toFixed(2)} s`;
}

function formatTimestamp(value) {
  const date = value ? new Date(value) : new Date();
  if (Number.isNaN(date.getTime())) return t("dynamic.now");
  return date.toLocaleTimeString(currentLanguage() === "zh" ? "zh-CN" : "en", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function shortID(value) {
  const text = safeText(value);
  return text.length > 15 ? `${text.slice(0, 7)}…${text.slice(-5)}` : text;
}

function truncateText(value, limit) {
  const text = safeText(value).trim();
  if (text.length <= limit) return text;
  return `${text.slice(0, limit).trimEnd()}…`;
}

function setText(id, value) {
  byId(id).textContent = value;
}
