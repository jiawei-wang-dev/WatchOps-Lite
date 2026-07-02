"use strict";

const state = {
  latestChatResponse: null,
  latestRequestId: "",
  latestTraceId: "",
  latestSessionId: "",
  latestSSEEvents: [],
  lastLatencyMS: null,
  selectedRating: "",
};

const sourceLabels = {
  metrics: "Metrics",
  logs: "Logs",
  traces: "Traces",
  knowledge: "Knowledge",
  alerts: "Alerts",
  topology: "Topology",
  memory: "Memory",
  long_term_memory: "Memory",
  unknown: "Other",
};

const presets = {
  "checkout error rate is high": "Why is the checkout error rate high in the last 20 minutes?",
  "payment timeout causing checkout failures": "Are payment timeouts causing checkout failures in the last 20 minutes?",
  "analyze slow trace": "Analyze slow checkout traces from the last 20 minutes and identify the bottleneck.",
  "search checkout runbook": "Find the checkout incident runbook and summarize the mitigation steps.",
  "check active checkout alerts": "Check active checkout alerts for the last 20 minutes.",
  "show checkout service topology": "Show the checkout service topology and relevant dependencies.",
};

const byId = (id) => document.getElementById(id);

document.addEventListener("DOMContentLoaded", () => {
  bindNavigation();
  bindActions();
  updateRuntimeContext();
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
      byId("message").value = presets[button.textContent.trim()] || button.textContent.trim();
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
      throw new Error(`HTTP ${response.status} returned an invalid JSON response.`);
    }
  }
  if (!response.ok) {
    const message = payload?.error?.message || payload?.message || `Request failed with HTTP ${response.status}.`;
    throw new Error(message);
  }
  return payload;
}

function buildChatPayload(streamSuffix = "") {
  const sessionID = byId("session-id").value.trim();
  const message = byId("message").value.trim();
  if (!sessionID || !message) {
    throw new Error("Session ID and reliability question are required.");
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
    setLoading(button, true, "Sending...");
    setResponseStatus("Running", "info");
    const started = performance.now();
    const response = await apiFetch("/api/v1/chat", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    state.lastLatencyMS = performance.now() - started;
    acceptChatResponse(response);
    setResponseStatus("Completed", "success");
    renderToast("Chat request completed.", "success");
  } catch (error) {
    setResponseStatus("Failed", "danger");
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, "Send request");
  }
}

async function sendStreamChat() {
  const buttons = [byId("send-stream"), byId("start-stream")];
  try {
    const payload = buildChatPayload("-stream");
    buttons.forEach((button) => setLoading(button, true, "Streaming..."));
    state.latestSSEEvents = [];
    byId("stream-timeline").innerHTML = "";
    setResponseStatus("Streaming", "info");
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
      let message = `Streaming failed with HTTP ${response.status}.`;
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
      throw new Error("Stream completed without a final answer.");
    }
    setResponseStatus("Completed", "success");
    updateRuntimeContext();
    renderToast("Streaming investigation completed.", "success");
  } catch (error) {
    setResponseStatus("Failed", "danger");
    renderSSEEvent("workflow_failed", {
      timestamp: new Date().toISOString(),
      message: error.message,
    });
    renderToast(error.message, "error");
  } finally {
    setLoading(byId("send-stream"), false, "Stream investigation");
    setLoading(byId("start-stream"), false, "Start stream");
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
    data = { message: "The event payload could not be decoded." };
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
  const messages = {
    workflow_started: `Workflow started for session ${safeText(data.session_id) || "current session"}.`,
    graph_node_started: `Graph node ${node || "unknown"} started.`,
    graph_node_completed: `Graph node ${node || "unknown"} completed.`,
    memory_loaded: `Session context loaded${data.available === false ? " with limited availability" : ""}.`,
    tool_call_started: `Tool ${tool || "unknown"} started.`,
    tool_call_completed: `Tool ${tool || "unknown"} completed${latency !== null ? ` in ${formatLatency(latency)}` : ""}${evidenceCount !== null ? ` with ${evidenceCount} evidence item(s)` : ""}.`,
    tool_call_failed: `Tool ${tool || "unknown"} failed${errorCode ? ` with ${errorCode}` : ""}.`,
    evidence_collected: `${evidenceCount ?? 0} evidence item(s) collected.`,
    failure_controller_triggered: `Failure controller applied a bounded safety response${errorCode ? ` (${errorCode})` : ""}.`,
    final_answer: "Final structured answer received.",
    workflow_completed: "Workflow completed.",
    workflow_failed: `Workflow failed${errorCode ? ` with ${errorCode}` : ""}${data.message ? `: ${safeText(data.message)}` : "."}`,
  };
  return messages[type] || `Operational event: ${type}.`;
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
}

function renderChatResponse(response) {
  const answer = response?.answer || {};
  const evidence = safeArray(answer.evidence);
  const toolRuns = safeArray(response?.tool_runs);
  const inferences = safeArray(answer.inferences);
  const limitations = safeArray(answer.limitations);
  byId("response-summary").innerHTML = metricCards([
    ["Evidence", evidence.length],
    ["Tool runs", toolRuns.length],
    ["Inferences", inferences.length],
    ["Limitations", limitations.length],
  ]);

  const sections = [
    renderStatementSection("Conclusions", answer.conclusion, "No conclusions returned."),
    renderCompactEvidence(evidence),
    renderStatementSection("Inferences", inferences, "No inferences returned."),
    renderStatementSection("Recommendations", answer.recommendations, "No recommendations returned."),
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
          ${safeArray(item?.evidence_ids).length ? `<span class="evidence-refs">Evidence: ${escapeHtml(item.evidence_ids.join(", "))}</span>` : ""}
        </li>`).join("")}</ul>`
    : `<p class="subtle">${escapeHtml(emptyMessage)}</p>`;
  return `<section class="response-section"><h4>${escapeHtml(title)}</h4>${content}</section>`;
}

function renderCompactEvidence(items) {
  const content = items.length
    ? `<ul class="response-list">${items.slice(0, 6).map((item) => `
        <li>
          <span class="source-badge">${escapeHtml(sourceLabel(item?.source_type))}</span>
          ${escapeHtml(item?.content || "Evidence content unavailable.")}
          <span class="evidence-refs">${escapeHtml(item?.id || "No evidence ID")}</span>
        </li>`).join("")}</ul>`
    : `<p class="subtle">No evidence returned. Review limitations before drawing conclusions.</p>`;
  return `<section class="response-section"><h4>Evidence</h4>${content}</section>`;
}

function renderLimitationSection(items) {
  const content = items.length
    ? `<ul class="response-list">${items.map((item) => `
        <li class="warning-row">
          <strong>${escapeHtml(item?.code || "LIMITATION")}</strong> · ${escapeHtml(item?.message || "No detail provided.")}
          ${item?.tool ? `<span class="evidence-refs">Tool: ${escapeHtml(item.tool)}</span>` : ""}
        </li>`).join("")}</ul>`
    : `<p class="subtle">No limitations reported.</p>`;
  return `<section class="response-section"><h4>Limitations</h4>${content}</section>`;
}

function renderMetadata(metadata) {
  return `<section class="response-section">
    <h4>Metadata</h4>
    <details>
      <summary>View structured metadata</summary>
      <pre>${escapeHtml(safeJson(metadata || {}))}</pre>
    </details>
  </section>`;
}

function renderToolRuns(toolRuns) {
  const runs = safeArray(toolRuns);
  if (!runs.length) {
    byId("tool-runs").innerHTML = `<div class="empty-state compact"><p>No tool runs available.</p></div>`;
  } else {
    byId("tool-runs").innerHTML = `
      <table>
        <thead><tr><th>Tool</th><th>Status</th><th>Latency</th><th>Evidence</th><th>Fallback / Error</th></tr></thead>
        <tbody>${runs.map((run) => {
          const failed = run?.success === false;
          const fallback = Number(run?.warning_count || 0) > 0;
          const status = failed ? "Failed" : fallback ? "Fallback" : "Success";
          const tone = failed ? "danger" : fallback ? "warning" : "success";
          const detail = run?.error_code || (fallback ? `${run.warning_count} warning(s)` : "—");
          return `<tr>
            <td><strong>${escapeHtml(run?.tool || "unknown")}</strong></td>
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
    container.innerHTML = `<div class="card empty-state"><h4>No evidence collected yet</h4><p>The Agent should report limitations instead of claiming an observed root cause.</p></div>`;
    renderEvidenceSummary();
    return;
  }
  container.innerHTML = Object.entries(groups).map(([source, values]) => `
    <article class="card">
      <div class="card-heading">
        <h3>${escapeHtml(sourceLabel(source))}</h3>
        <span class="source-badge">${values.length} item${values.length === 1 ? "" : "s"}</span>
      </div>
      ${values.map((item) => `
        <div class="evidence-row">
          <p>${escapeHtml(truncateText(item?.content || "Evidence content unavailable.", 520))}</p>
          <div class="evidence-meta">
            <span>${escapeHtml(item?.id || "no-id")}</span>
            ${item?.source_name ? `<span>${escapeHtml(item.source_name)}</span>` : ""}
            ${item?.score !== undefined ? `<span>score ${escapeHtml(String(item.score))}</span>` : ""}
          </div>
          ${item?.metadata ? `<details><summary>Evidence metadata</summary><pre>${escapeHtml(safeJson(item.metadata))}</pre></details>` : ""}
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
    ["Tool runs", runs.length],
    ["Successful", successful],
    ["Failed", runs.length - successful],
    ["Evidence", evidence.length],
    ["Limitations", limitations.length],
    ["Trace", trace ? shortID(trace) : "—"],
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
    renderToast("Enter a knowledge query.", "error");
    return;
  }
  try {
    setLoading(button, true, "Searching...");
    const response = await apiFetch("/api/v1/knowledge/search", {
      method: "POST",
      body: JSON.stringify({ query, limit: 5, filters: {} }),
    });
    renderKnowledgeResults(response?.results);
    renderToast("Knowledge search completed.", "success");
  } catch (error) {
    byId("knowledge-results").innerHTML = polishedEmpty(error.message);
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, "Search");
  }
}

function renderKnowledgeResults(results) {
  const items = safeArray(results);
  if (!items.length) {
    byId("knowledge-results").innerHTML = polishedEmpty("No matching knowledge chunks were returned.");
    return;
  }
  byId("knowledge-results").innerHTML = items.map((item) => `
    <article class="search-result">
      <h4>${escapeHtml(item?.title || "Untitled knowledge")}</h4>
      <p>${escapeHtml(truncateText(item?.content || "No content snippet.", 420))}</p>
      <div class="result-meta">
        <span>document ${escapeHtml(item?.document_id || "—")}</span>
        <span>chunk ${escapeHtml(item?.chunk_id || "—")}</span>
        <span>score ${escapeHtml(String(item?.score ?? "—"))}</span>
        ${item?.metadata?.retrieval_mode ? `<span>${escapeHtml(item.metadata.retrieval_mode)}</span>` : ""}
      </div>
      ${item?.metadata ? `<details><summary>Retrieval metadata</summary><pre>${escapeHtml(safeJson(item.metadata))}</pre></details>` : ""}
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
      <article><span>Session memory</span><strong>${availabilityLabel(sessionAvailable)}</strong></article>
      <article><span>Long-term memory</span><strong>${availabilityLabel(longTermAvailable)}</strong></article>
    </div>
    ${memoryEvidence.length
      ? memoryEvidence.map((item) => `<div class="evidence-row"><p>${escapeHtml(item?.content || "Memory content unavailable.")}</p><div class="evidence-meta"><span>${escapeHtml(item?.id || "memory")}</span></div></div>`).join("")
      : `<div class="inline-note">No memory evidence is exposed in the latest Chat response. No additional memory API is queried by this console.</div>`}
    <details><summary>Visible Chat metadata</summary><pre>${escapeHtml(safeJson(metadata))}</pre></details>`;
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
    byId("feedback-result").textContent = "Feedback requires a completed chat response.";
  } else if (!state.selectedRating) {
    byId("feedback-result").textContent = "Choose Helpful or Needs work to submit feedback.";
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
    setLoading(button, true, "Submitting...");
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
    byId("feedback-result").textContent = `Feedback ${result.feedback_id || ""} created successfully.`;
    renderToast("Feedback submitted.", "success");
  } catch (error) {
    byId("feedback-result").textContent = `Feedback unavailable: ${error.message}`;
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, "Submit feedback");
    updateFeedbackAvailability();
  }
}

async function runEval() {
  const button = byId("run-eval");
  const caseType = byId("eval-case-type").value;
  const limit = Math.max(1, Math.min(20, Number(byId("eval-limit").value) || 5));
  try {
    setLoading(button, true, "Running...");
    const result = await apiFetch("/api/v1/eval/runs", {
      method: "POST",
      body: JSON.stringify({ case_type: caseType, limit }),
    });
    byId("eval-result").className = "";
    byId("eval-result").innerHTML = `
      <div class="memory-status-grid">
        <article><span>Total</span><strong>${escapeHtml(String(result?.total ?? 0))}</strong></article>
        <article><span>Passed</span><strong>${escapeHtml(String(result?.passed ?? 0))}</strong></article>
        <article><span>Failed</span><strong>${escapeHtml(String(result?.failed ?? 0))}</strong></article>
        <article><span>Status</span><strong>${escapeHtml(result?.status || "unknown")}</strong></article>
      </div>
      <div class="inline-note">Run ID: ${escapeHtml(result?.run_id || "not returned")}</div>`;
    renderToast("Eval run completed.", "success");
  } catch (error) {
    byId("eval-result").innerHTML = polishedEmpty(`Eval unavailable: ${error.message}`);
    renderToast(error.message, "error");
  } finally {
    setLoading(button, false, "Run eval");
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
    ? runs.slice(-5).map((run) => `<div class="mini-tool"><span>${escapeHtml(run?.tool || "unknown")}</span><span class="badge ${run?.success === false ? "danger" : "success"}">${run?.success === false ? "failed" : "ok"}</span></div>`).join("")
    : `<span class="subtle">No tool runs.</span>`;
}

function memoryPill(name, available) {
  const label = available === true ? "ready" : available === false ? "limited" : "unknown";
  return `<span class="memory-pill ${available === true ? "available" : ""}">${escapeHtml(name)} · ${label}</span>`;
}

function setCopyValue(id, value) {
  const element = byId(id);
  element.textContent = value ? shortID(value) : "—";
  element.dataset.copy = value || "";
  element.title = value ? `Copy ${value}` : "No value to copy";
}

async function copyToClipboard(value) {
  if (!value) {
    renderToast("No ID is available to copy.", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
    renderToast("Copied to clipboard.", "success");
  } catch {
    renderToast("Clipboard access is unavailable.", "error");
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
  return sourceLabels[key] || key.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function availabilityLabel(value) {
  if (value === true) return "Available";
  if (value === false) return "Unavailable / degraded";
  return "Not exposed";
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
    return JSON.stringify({ message: "Metadata could not be serialized." }, null, 2);
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
  if (Number.isNaN(date.getTime())) return "now";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
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
