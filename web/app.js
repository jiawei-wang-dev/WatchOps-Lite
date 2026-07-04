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
  latestStreamResponse: null,
  latestMultiAgentResponse: null,
  chatMode: "single",
  streamStatus: "not_started",
  streamStartedAt: "",
  streamEndedAt: "",
  lastRequestStatus: "unknown",
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

const streamStepDefinitions = [
  {
    id: "input",
    label: "step.input",
    description: "step.input_desc",
    nodes: ["normalize_chat_input"],
  },
  {
    id: "context",
    label: "step.context",
    description: "step.context_desc",
    nodes: [
      "load_session_context",
      "load_long_term_memory",
      "load_user_profile",
      "prepare_diagnostic_skills",
    ],
    events: ["memory_loaded"],
  },
  {
    id: "prompt",
    label: "step.prompt",
    description: "step.prompt_desc",
    nodes: ["merge_context", "render_prompt_template"],
  },
  {
    id: "agent",
    label: "step.agent",
    description: "step.agent_desc",
    nodes: ["run_react_agent"],
  },
  {
    id: "tools",
    label: "step.tools",
    description: "step.tools_desc",
    events: ["tool_call_started", "tool_call_completed", "tool_call_failed"],
  },
  {
    id: "evidence",
    label: "step.evidence",
    description: "step.evidence_desc",
    nodes: ["collect_tool_evidence"],
    events: ["evidence_collected"],
  },
  {
    id: "memory",
    label: "step.memory",
    description: "step.memory_desc",
    nodes: ["persist_session_memory"],
  },
  {
    id: "answer",
    label: "step.answer",
    description: "step.answer_desc",
    nodes: ["build_chat_response"],
    events: ["final_answer", "workflow_completed", "workflow_failed"],
  },
];

const multiAgentStepDefinitions = [
  { id: "triage", label: "multi.role_triage", description: "multi.role_triage_desc", role: "triage" },
  { id: "evidence", label: "multi.role_evidence", description: "multi.role_evidence_desc", role: "evidence" },
  { id: "knowledge", label: "multi.role_knowledge", description: "multi.role_knowledge_desc", role: "knowledge" },
  { id: "merge", label: "multi.role_merge", description: "multi.role_merge_desc", role: "merge" },
  { id: "synthesis", label: "multi.role_synthesis", description: "multi.role_synthesis_desc", role: "synthesis" },
  { id: "answer", label: "step.answer", description: "step.answer_desc", events: ["final_answer", "multi_agent_completed", "multi_agent_failed"] },
];

const multiAgentRoles = ["triage", "evidence", "knowledge", "synthesis"];
const multiAgentModeLabels = {
  triage: "multi.mode_triage",
  evidence: "multi.mode_evidence",
  knowledge: "multi.mode_knowledge",
  synthesis: "multi.mode_synthesis",
};

const evidenceSourceOrder = [
  "metrics",
  "logs",
  "alerts",
  "knowledge",
  "topology",
  "traces",
  "memory",
  "long_term_memory",
  "unknown",
];

const toolSourceTypes = {
  query_metrics: ["metrics"],
  query_logs: ["logs"],
  query_alerts: ["alerts"],
  query_traces: ["traces"],
  search_knowledge: ["knowledge"],
  get_service_topology: ["topology"],
};

const byId = (id) => document.getElementById(id);
const t = (key, replacements = {}) => window.WatchOpsI18n?.t(key, replacements) || key;
const currentLanguage = () => window.WatchOpsI18n?.getLanguage() || "zh";

document.addEventListener("DOMContentLoaded", () => {
  bindNavigation();
  bindActions();
  updateRuntimeContext();
  renderStreamDashboard();
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
  document.querySelectorAll("[data-agent-mode]").forEach((button) => {
    button.addEventListener("click", () => setChatMode(button.dataset.agentMode));
  });
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

function setChatMode(mode) {
  state.chatMode = mode === "multi" ? "multi" : "single";
  document.querySelectorAll("[data-agent-mode]").forEach((button) => {
    const selected = button.dataset.agentMode === state.chatMode;
    button.classList.toggle("active", selected);
    button.setAttribute("aria-pressed", String(selected));
  });
  byId("multi-agent-panel").hidden = state.chatMode !== "multi";
  renderMultiAgentSteps(state.latestMultiAgentResponse);
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
  renderMultiAgentSteps(state.latestMultiAgentResponse);
  if (state.latestKnowledgeResults !== null) {
    renderKnowledgeResults(state.latestKnowledgeResults);
  }
  if (state.latestHistoryResponse) {
    renderHistory(state.latestHistoryResponse);
  }
  if (state.latestSSEEvents.length) {
    byId("stream-timeline").innerHTML = "";
    state.latestSSEEvents.forEach((event) => renderSSEEvent(
      event.type,
      withEventTimestamp(event),
    ));
  }
  renderStreamDashboard();
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
    state.lastRequestStatus = "running";
    renderRuntimeStatuses();
    const payload = buildChatPayload();
    setLoading(button, true, t("common.sending"));
    setResponseStatus(t("common.running"), "info");
    const started = performance.now();
    const endpoint = state.chatMode === "multi" ?
      "/api/v1/chat/multi-agent" : "/api/v1/chat";
    const response = await apiFetch(endpoint, {
      method: "POST",
      body: JSON.stringify(payload),
    });
    state.lastLatencyMS = performance.now() - started;
    state.latestMultiAgentResponse = state.chatMode === "multi" ? response : null;
    acceptChatResponse(response);
    renderMultiAgentSteps(state.latestMultiAgentResponse);
    setResponseStatus(t("common.completed"), "success");
    renderToast(t("dynamic.chat_completed"), "success");
  } catch (error) {
    state.lastRequestStatus = "error";
    renderRuntimeStatuses();
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
    resetStreamView();
    setResponseStatus(t("common.streaming"), "info");
    const started = performance.now();
    const endpoint = state.chatMode === "multi" ?
      "/api/v1/chat/multi-agent/stream" : "/api/v1/chat/stream";
    const response = await fetch(endpoint, {
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
    if (state.streamStatus !== "failed") state.streamStatus = "completed";
    if (!state.streamEndedAt) state.streamEndedAt = new Date().toISOString();
    renderStreamDashboard();
    updateRuntimeContext();
    renderToast(t("dynamic.stream_completed"), "success");
  } catch (error) {
    state.lastRequestStatus = "error";
    setResponseStatus(t("common.failed"), "danger");
    const failedAt = new Date().toISOString();
    const failureEvent = {
      type: "workflow_failed",
      data: { timestamp: failedAt, message: error.message },
      receivedAt: failedAt,
    };
    state.latestSSEEvents.push(failureEvent);
    updateStreamStatusFromEvent(failureEvent);
    renderSSEEvent(failureEvent.type, withEventTimestamp(failureEvent));
    state.streamStatus = "failed";
    state.streamEndedAt = failedAt;
    renderStreamDashboard();
    renderRuntimeStatuses();
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
  const event = {
    type: eventType,
    data,
    receivedAt: new Date().toISOString(),
  };
  state.latestSSEEvents.push(event);
  updateStreamStatusFromEvent(event);
  renderSSEEvent(eventType, withEventTimestamp(event));
  if (eventType === "final_answer" && data && typeof data === "object") {
    state.latestStreamResponse = data;
    state.latestMultiAgentResponse = data?.mode === "multi_agent" ? data : null;
    acceptChatResponse(data);
    renderMultiAgentSteps(state.latestMultiAgentResponse);
  }
  renderStreamDashboard();
  return eventType;
}

function resetStreamView() {
  state.latestSSEEvents = [];
  state.latestStreamResponse = null;
  if (state.chatMode === "multi") state.latestMultiAgentResponse = null;
  state.streamStatus = "running";
  state.streamStartedAt = new Date().toISOString();
  state.streamEndedAt = "";
  state.lastRequestStatus = "running";
  byId("stream-timeline").innerHTML = "";
  byId("raw-sse-details").open = false;
  renderStreamDashboard();
}

function updateStreamStatusFromEvent(event) {
  const timestamp = eventTimestamp(event);
  if (!state.streamStartedAt ||
      event.type === "workflow_started" ||
      event.type === "multi_agent_started") {
    state.streamStartedAt = timestamp;
  }
  switch (event.type) {
  case "workflow_failed":
  case "multi_agent_failed":
    state.streamStatus = "failed";
    state.streamEndedAt = timestamp;
    break;
  case "workflow_completed":
  case "multi_agent_completed":
    state.streamStatus = "completed";
    state.streamEndedAt = timestamp;
    break;
  default:
    if (state.streamStatus === "not_started") state.streamStatus = "running";
  }
}

function withEventTimestamp(event) {
  return {
    ...(event?.data || {}),
    timestamp: safeText(event?.data?.timestamp) || safeText(event?.receivedAt),
  };
}

function eventTimestamp(event) {
  return safeText(event?.data?.timestamp) ||
    safeText(event?.receivedAt) ||
    new Date().toISOString();
}

function renderStreamDashboard() {
  renderStreamSummary();
  renderStreamSteps();
  renderStreamToolTimeline();
  renderMultiAgentSteps(state.latestMultiAgentResponse);
  renderKeyEvidenceGroups(
    state.latestStreamResponse?.answer?.evidence,
    "stream-key-evidence",
  );
  setText("raw-event-count", String(state.latestSSEEvents.length));
  updateTraceGuide();
}

function renderStreamSummary() {
  const response = state.latestStreamResponse || {};
  const metadata = response?.metadata || {};
  const toolRuns = safeArray(response?.tool_runs);
  const evidence = safeArray(response?.answer?.evidence);
  const limitations = safeArray(response?.answer?.limitations);
  const completion = findLastStreamEvent("workflow_completed");
  const failure = findLastStreamEvent("workflow_failed");
  const status = failure ? "failed" : state.streamStatus;
  const fallbackUsed = metadata.fallback_used === true;
  const agentMode = safeText(metadata.agent_mode);
  const model = safeText(metadata.model);
  const latency = safeNumber(completion?.data?.latency_ms) ??
    safeNumber(failure?.data?.latency_ms) ??
    elapsedStreamMilliseconds();
  const requestID = safeText(response.request_id) ||
    latestStreamDataValue("request_id");
  const traceID = safeText(response.trace_id) ||
    latestStreamDataValue("trace_id");

  byId("stream-summary").innerHTML = metricCards([
    [t("common.status"), streamStatusLabel(status)],
    [
      t("stream.agent_mode"),
      agentMode === "eino_react" && !fallbackUsed ?
        t("stream.llm_mode") :
        agentMode === "deterministic" || fallbackUsed ?
          t("stream.deterministic_mode") : "—",
    ],
    [
      t("stream.model"),
      model || (fallbackUsed || agentMode === "deterministic" ?
        t("stream.fallback_model") : "—"),
    ],
    [t("stream.total_latency"), latency === null ? "—" : formatLatency(latency)],
    [t("common.tool_runs"), toolRuns.length || countCompletedToolEvents()],
    [t("common.evidence"), evidence.length || latestEvidenceCount()],
    [t("common.limitations"), limitations.length],
    ["fallback_used", String(fallbackUsed)],
  ]);

  const statusBadge = byId("stream-summary-status");
  statusBadge.textContent = streamStatusLabel(status);
  statusBadge.className = `badge ${statusTone(status)}`;

  const notice = byId("stream-agent-notice");
  let noticeKey = "stream.summary_empty";
  let noticeTone = "";
  if (status === "running") {
    noticeKey = "stream.running_notice";
    noticeTone = "info-note";
  } else if (status === "failed") {
    noticeKey = "stream.failed_notice";
    noticeTone = "danger-note";
  } else if (fallbackUsed) {
    noticeKey = "stream.fallback_notice";
    noticeTone = "warning-note";
  } else if (agentMode === "deterministic") {
    noticeKey = "stream.deterministic_notice";
    noticeTone = "warning-note";
  } else if (status === "completed" && agentMode === "eino_react") {
    noticeKey = "stream.llm_completed_notice";
    noticeTone = "success-note";
  }
  notice.textContent = t(noticeKey);
  notice.className = `inline-note ${noticeTone}`.trim();

  byId("stream-identifiers").innerHTML = `
    <span>request_id: <code>${escapeHtml(requestID || "—")}</code></span>
    <span>trace_id: <code>${escapeHtml(traceID || "—")}</code></span>`;
}

function renderStreamSteps() {
  const container = byId("stream-key-steps");
  const definitions = state.chatMode === "multi" ?
    multiAgentStepDefinitions : streamStepDefinitions;
  container.innerHTML = definitions.map((definition, index) => {
    const aggregate = aggregateStreamStep(definition);
    return `
      <article class="step-card ${escapeHtml(aggregate.status)}">
        <div class="step-index">${index + 1}</div>
        <div class="step-content">
          <div class="step-heading">
            <h4>${escapeHtml(t(definition.label))}</h4>
            <span class="badge ${statusTone(aggregate.status)}">${escapeHtml(streamStatusLabel(aggregate.status))}</span>
          </div>
          <p>${escapeHtml(t(definition.description))}</p>
          <div class="step-meta">
            <span>${escapeHtml(t("stream.start_time"))}: ${escapeHtml(formatTimestampOrDash(aggregate.startedAt))}</span>
            <span>${escapeHtml(t("stream.end_time"))}: ${escapeHtml(formatTimestampOrDash(aggregate.endedAt))}</span>
            <span>${escapeHtml(t("stream.duration"))}: ${aggregate.durationMS === null ? "—" : escapeHtml(formatLatency(aggregate.durationMS))}</span>
          </div>
        </div>
      </article>`;
  }).join("");
}

function aggregateStreamStep(definition) {
  const matches = state.latestSSEEvents.filter((event) => {
    const node = safeText(event?.data?.node || event?.data?.node_name);
    const role = safeText(event?.data?.role);
    return safeArray(definition.nodes).includes(node) ||
      (definition.role && definition.role === role) ||
      safeArray(definition.events).includes(event.type);
  });
  if (!matches.length) {
    return {
      status: state.streamStatus === "running" ? "waiting" : "untriggered",
      startedAt: "",
      endedAt: "",
      durationMS: null,
    };
  }
  const failure = matches.find((event) =>
    event.type === "workflow_failed" ||
    event.type === "multi_agent_failed" ||
    event.type === "tool_call_failed");
  const started = matches.filter((event) =>
    event.type.endsWith("_started") || event.type === "workflow_started");
  const completed = matches.filter((event) =>
    event.type.endsWith("_completed") ||
    event.type === "final_answer" ||
    event.type === "memory_loaded" ||
    event.type === "evidence_collected");
  const startedAt = eventTimestamp(started[0] || matches[0]);
  const lifecycleIncomplete = started.length > completed.filter((event) =>
    event.type.endsWith("_completed")).length;
  const endedEvent = lifecycleIncomplete ? null :
    completed[completed.length - 1] ||
    (failure ? matches[matches.length - 1] : null);
  const endedAt = endedEvent ? eventTimestamp(endedEvent) : "";
  const status = failure ? "failed" :
    endedEvent ? "completed" : "running";
  return {
    status,
    startedAt,
    endedAt,
    durationMS: durationBetween(startedAt, endedAt),
  };
}

function renderStreamToolTimeline() {
  const container = byId("stream-tool-timeline");
  const calls = buildStreamToolCalls();
  if (!calls.length) {
    container.innerHTML = polishedEmpty(t("evidence.no_tools"));
    return;
  }
  container.innerHTML = calls.map((call, index) => {
    const evidence = evidenceForTool(call.tool).slice(0, 3);
    const evidenceCount = safeNumber(call.evidenceCount) ?? evidence.length;
    const warningCount = safeNumber(call.warningCount) ?? 0;
    const failed = call.success === false || Boolean(call.errorCode);
    const noData = !failed && call.completed && evidenceCount === 0;
    const status = failed ? "failed" :
      noData ? "no_data" :
        call.completed ? "completed" : "running";
    const summaries = evidence.length
      ? `<ul>${evidence.map((item) =>
        `<li>${escapeHtml(truncateText(item?.content || t("dynamic.evidence_unavailable"), 180))}</li>`
      ).join("")}</ul>`
      : `<p class="subtle">${escapeHtml(t("stream.no_evidence"))}</p>`;
    const detail = {
      sequence: index + 1,
      tool: call.tool,
      status,
      latency_ms: call.latencyMS,
      evidence_count: evidenceCount,
      warning_count: warningCount,
      error_code: call.errorCode || "",
    };
    return `
      <article class="tool-call-card ${failed ? "failed" : ""}">
        <div class="tool-call-heading">
          <span class="tool-sequence">${index + 1}</span>
          <div>
            <h4><code>${escapeHtml(call.tool || t("common.unknown"))}</code></h4>
            <span class="badge ${statusTone(status)}">${escapeHtml(streamStatusLabel(status))}</span>
          </div>
          <strong>${call.latencyMS === null ? "—" : escapeHtml(formatLatency(call.latencyMS))}</strong>
        </div>
        <div class="tool-call-stats">
          <span>${escapeHtml(t("tool.evidence_count", { count: evidenceCount }))}</span>
          <span>${escapeHtml(t("tool.warning_count", { count: warningCount }))}</span>
          ${call.errorCode ? `<span class="danger-text">${escapeHtml(t("tool.error", { error: call.errorCode }))}</span>` : ""}
        </div>
        <div class="tool-evidence-summary">
          <b>${escapeHtml(t("tool.summary"))}</b>
          ${summaries}
        </div>
        <details>
          <summary>${escapeHtml(t("tool.metadata"))}</summary>
          <pre>${escapeHtml(safeJson(detail))}</pre>
        </details>
      </article>`;
  }).join("");
}

function buildStreamToolCalls() {
  const calls = [];
  state.latestSSEEvents.forEach((event) => {
    if (!event.type.startsWith("tool_call_")) return;
    const tool = safeText(event?.data?.tool || event?.data?.tool_name) ||
      t("common.unknown");
    if (event.type === "tool_call_started") {
      calls.push({
        tool,
        startedAt: eventTimestamp(event),
        endedAt: "",
        completed: false,
        success: null,
        latencyMS: null,
        evidenceCount: null,
        warningCount: null,
        errorCode: "",
      });
      return;
    }
    let current = [...calls].reverse().find((call) =>
      call.tool === tool && !call.completed);
    if (!current) {
      current = {
        tool,
        startedAt: eventTimestamp(event),
        completed: false,
      };
      calls.push(current);
    }
    current.completed = true;
    current.success = event.type !== "tool_call_failed";
    current.endedAt = eventTimestamp(event);
    current.latencyMS = safeNumber(event?.data?.latency_ms) ??
      durationBetween(current.startedAt, current.endedAt);
    current.evidenceCount = safeNumber(event?.data?.evidence_count);
    current.warningCount = safeNumber(event?.data?.warning_count);
    current.errorCode = safeText(
      event?.data?.error_code || event?.data?.error_type,
    );
  });

  safeArray(state.latestStreamResponse?.tool_runs).forEach((run, index) => {
    const current = calls[index] || {
      tool: safeText(run?.tool),
      startedAt: "",
      endedAt: "",
    };
    if (!calls[index]) calls.push(current);
    current.tool = safeText(run?.tool) || current.tool;
    current.completed = true;
    current.success = run?.success !== false;
    current.latencyMS = safeNumber(run?.duration_ms) ?? current.latencyMS ?? null;
    current.evidenceCount = safeNumber(run?.evidence_count) ??
      current.evidenceCount ?? null;
    current.warningCount = safeNumber(run?.warning_count) ??
      current.warningCount ?? 0;
    current.errorCode = safeText(run?.error_code) || current.errorCode || "";
  });
  return calls;
}

function evidenceForTool(toolName) {
  const sourceTypes = toolSourceTypes[toolName] || [];
  return safeArray(state.latestStreamResponse?.answer?.evidence).filter((item) =>
    sourceTypes.includes(safeText(item?.source_type).toLowerCase()));
}

function renderKeyEvidenceGroups(evidence, containerID) {
  const container = byId(containerID);
  if (!container) return;
  const items = safeArray(evidence);
  if (!items.length) {
    container.innerHTML = polishedEmpty(t("stream.no_evidence"));
    return;
  }
  const groups = groupBySourceType(items);
  const orderedSources = evidenceSourceOrder.filter((source) => groups[source]);
  Object.keys(groups).forEach((source) => {
    if (!orderedSources.includes(source)) orderedSources.push(source);
  });
  container.innerHTML = orderedSources.map((source) => {
    const values = groups[source].slice(0, 3);
    return `
      <article class="key-evidence-card">
        <div class="card-heading">
          <h4>${escapeHtml(sourceLabel(source))}</h4>
          <span class="source-badge">${escapeHtml(t("dynamic.item_count", { count: values.length }))}</span>
        </div>
        ${values.map((item) => `
          <div class="key-evidence-item">
            <code>${escapeHtml(item?.id || t("dynamic.no_evidence_id"))}</code>
            <p>${escapeHtml(truncateText(item?.content || t("dynamic.evidence_unavailable"), 220))}</p>
            <div class="evidence-meta">
              <span>source_type: ${escapeHtml(item?.source_type || source)}</span>
              ${item?.resource_id ? `<span>${escapeHtml(t("evidence.resource"))}: ${escapeHtml(item.resource_id)}</span>` : ""}
              ${item?.score !== undefined ? `<span>${escapeHtml(t("evidence.score"))}: ${escapeHtml(String(item.score))}</span>` : ""}
            </div>
            ${item?.metadata ? `<details><summary>${escapeHtml(t("dynamic.evidence_metadata"))}</summary><pre>${escapeHtml(safeJson(item.metadata))}</pre></details>` : ""}
          </div>`).join("")}
      </article>`;
  }).join("");
}

function findLastStreamEvent(type) {
  return [...state.latestSSEEvents].reverse().find((event) =>
    event.type === type);
}

function latestStreamDataValue(key) {
  for (let index = state.latestSSEEvents.length - 1; index >= 0; index--) {
    const value = safeText(state.latestSSEEvents[index]?.data?.[key]);
    if (value) return value;
  }
  return "";
}

function latestEvidenceCount() {
  const event = findLastStreamEvent("evidence_collected");
  return safeNumber(event?.data?.evidence_count) ?? 0;
}

function countCompletedToolEvents() {
  return state.latestSSEEvents.filter((event) =>
    event.type === "tool_call_completed" ||
    event.type === "tool_call_failed").length;
}

function elapsedStreamMilliseconds() {
  if (!state.streamStartedAt) return null;
  const end = state.streamEndedAt || new Date().toISOString();
  return durationBetween(state.streamStartedAt, end);
}

function durationBetween(start, end) {
  if (!start || !end) return null;
  const duration = new Date(end).getTime() - new Date(start).getTime();
  return Number.isFinite(duration) && duration >= 0 ? duration : null;
}

function formatTimestampOrDash(value) {
  return value ? formatTimestamp(value) : "—";
}

function streamStatusLabel(status) {
  const labels = {
    not_started: "stream.not_started",
    waiting: "stream.waiting",
    running: "common.running",
    completed: "common.completed",
    failed: "common.failed",
    untriggered: "stream.untriggered",
    no_data: "stream.no_data",
    pending: "stream.pending",
    skipped: "stream.skipped",
  };
  return t(labels[status] || "common.unknown");
}

function statusTone(status) {
  if (status === "completed") return "success";
  if (status === "failed") return "danger";
  if (status === "no_data" || status === "untriggered") return "warning";
  if (status === "running") return "info";
  return "neutral";
}

function updateTraceGuide() {
  const hint = byId("trace-guide-hint");
  if (!hint) return;
  hint.textContent = t("trace.open_hint");
  hint.classList.toggle("highlight", Boolean(state.latestTraceId));
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
  if (byId("raw-sse-details").open) {
    event.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }
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
  const role = safeText(data.role) || t("common.unknown");
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
    multi_agent_started: t("event.multi_agent_started"),
    agent_step_started: t("event.agent_step_started", { role }),
    agent_step_completed: t("event.agent_step_completed", { role }),
    synthesis_started: t("event.synthesis_started"),
    agent_llm_started: t("event.agent_llm_started", { role }),
    agent_llm_completed: t("event.agent_llm_completed", {
      role,
      model: safeText(data.model) || t("common.unknown"),
      latency: latency === null ? "—" : formatLatency(latency),
    }),
    agent_llm_failed: t("event.agent_llm_failed", { role, code: codeText }),
    multi_agent_completed: t("event.multi_agent_completed"),
    multi_agent_failed: t("event.multi_agent_failed", {
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

function renderMultiAgentSteps(response) {
  const panel = byId("multi-agent-panel");
  const container = byId("multi-agent-steps");
  if (!panel || !container) return;
  panel.hidden = state.chatMode !== "multi";
  if (panel.hidden) return;

  const steps = safeArray(response?.agent_steps);
  const status = byId("multi-agent-status");
  const llmCount = byId("multi-agent-llm-count");
  if (llmCount) {
    llmCount.textContent = String(
      safeNumber(response?.metadata?.multi_agent_llm_call_count) ?? 0,
    );
  }
  const workflowStatus = steps.length ? "completed" :
    state.streamStatus === "running" && state.latestSSEEvents.some((event) =>
      event.type === "multi_agent_started") ? "running" : "pending";
  status.textContent = streamStatusLabel(workflowStatus);
  status.className = `badge ${statusTone(workflowStatus)}`;
  container.innerHTML = multiAgentRoles.map((role) => {
    const step = steps.find((item) => safeText(item?.role) === role);
    const startedEvent = state.latestSSEEvents.find((event) =>
      event.type === "agent_step_started" &&
      safeText(event?.data?.role) === role);
    const completedEvent = [...state.latestSSEEvents].reverse().find((event) =>
      event.type === "agent_step_completed" &&
      safeText(event?.data?.role) === role);
    const stepStatus = safeText(step?.status) ||
      (completedEvent ? "completed" : startedEvent ? "running" : "pending");
    const toolNames = safeArray(step?.tool_runs)
      .map((run) => safeText(run?.tool))
      .filter(Boolean);
    const liveEvidenceCount = safeNumber(completedEvent?.data?.evidence_count);
    const evidenceCount = step ?
      safeArray(step.evidence_ids).length : liveEvidenceCount ?? 0;
    const duration = step ?
      safeNumber(step.duration_ms) : safeNumber(completedEvent?.data?.latency_ms);
    const metadata = step?.metadata || {};
    const prefix = role;
    const llmUsed = metadata[`${prefix}_llm_used`] === true;
    const llmFallback = metadata[`${prefix}_fallback_used`] === true;
    const modelName = safeText(metadata[`${prefix}_model`]);
    const llmDuration = llmUsed ?
      safeNumber(metadata[`${prefix}_llm_duration_ms`]) : null;
    const analysisMode = llmUsed ? t(multiAgentModeLabels[role]) :
      role === "triage" ? t("multi.rule_based") :
      llmFallback ? t("multi.llm_fallback") : t(multiAgentModeLabels[role]);
    const output = safeText(step?.output);
    return `
      <article class="agent-role-card ${escapeHtml(stepStatus)}">
        <div class="agent-role-heading">
          <div>
            <span class="agent-role-index">${multiAgentRoles.indexOf(role) + 1}</span>
            <h4>${escapeHtml(t(`multi.role_${role}`))}</h4>
          </div>
          <span class="badge ${statusTone(stepStatus)}">${escapeHtml(streamStatusLabel(stepStatus))}</span>
        </div>
        <p class="agent-role-desc">${escapeHtml(t(`multi.role_${role}_desc`))}</p>
        <dl class="agent-role-facts">
          <div><dt>${escapeHtml(t("stream.duration"))}</dt><dd>${duration === null ? "—" : escapeHtml(formatLatency(duration))}</dd></div>
          <div><dt>${escapeHtml(t("common.evidence"))}</dt><dd>${evidenceCount}</dd></div>
          <div><dt>${escapeHtml(t("common.limitations"))}</dt><dd>${safeArray(step?.limitations).length}</dd></div>
        </dl>
        <div class="agent-role-field">
          <span>${escapeHtml(t("common.tool_runs"))}</span>
          <div class="tool-chip-list">${toolNames.length ?
            toolNames.map((tool) => `<code class="tool-chip">${escapeHtml(tool)}</code>`).join("") :
            `<span class="subtle">—</span>`}
          </div>
        </div>
        <dl class="agent-role-runtime">
          <div><dt>${escapeHtml(t("multi.analysis_mode"))}</dt><dd>${escapeHtml(analysisMode)}</dd></div>
          <div><dt>${escapeHtml(t("multi.model"))}</dt><dd>${escapeHtml(modelName || "—")}</dd></div>
          <div><dt>${escapeHtml(t("multi.llm_latency"))}</dt><dd>${llmDuration === null ? "—" : escapeHtml(formatLatency(llmDuration))}</dd></div>
          <div><dt>${escapeHtml(t("multi.fallback"))}</dt><dd>${escapeHtml(llmFallback ? t("common.yes") : t("common.no"))}</dd></div>
        </dl>
        ${output ? `<details class="agent-output-details"><summary><span>${escapeHtml(t("multi.step_output"))}</span><span class="when-closed">${escapeHtml(t("multi.expand"))}</span><span class="when-open">${escapeHtml(t("multi.collapse"))}</span></summary><p>${escapeHtml(output)}</p></details>` : ""}
      </article>`;
  }).join("");
}

function acceptChatResponse(response) {
  state.latestChatResponse = response || {};
  state.latestRequestId = safeText(response?.request_id);
  state.latestTraceId = safeText(response?.trace_id);
  state.latestSessionId = safeText(response?.session_id);
  state.lastRequestStatus = "success";
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
      <div class="inline-note warning-note tool-fallback-note">${escapeHtml(t("tool.degraded_explanation"))}</div>
      <table>
        <thead><tr><th>${escapeHtml(t("dynamic.tool"))}</th><th>${escapeHtml(t("common.status"))}</th><th>${escapeHtml(t("dynamic.latency"))}</th><th>${escapeHtml(t("common.evidence"))}</th><th>${escapeHtml(t("dynamic.fallback_error"))}</th></tr></thead>
        <tbody>${runs.map((run) => {
          const failed = run?.success === false;
          const fallback = toolRunUsedFallback(run) || Number(run?.warning_count || 0) > 0;
          const status = failed ? t("tool.status_failed") :
            fallback ? t("tool.status_degraded") : t("tool.status_success");
          const tone = failed ? "danger" : fallback ? "warning" : "success";
          const detail = run?.error_code || (fallback ? t("dynamic.warning_count", {
            count: run.warning_count ?? 0,
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
  renderKeyEvidenceGroups(items, "key-evidence-groups");
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
  const duplicateIndexes = findDuplicateKnowledgeResults(items);
  const hiddenDuplicateCount = items.reduce((total, item) => {
    const count = safeNumber(item?.metadata?.deduped_duplicate_count);
    return total + (count === null ? 0 : count);
  }, 0);
  byId("knowledge-results").innerHTML = `
    <div class="knowledge-results-heading">
      <h3>${escapeHtml(t("knowledge.results"))}</h3>
      <span class="source-badge">${escapeHtml(t("dynamic.item_count", { count: items.length }))}</span>
    </div>
    ${hiddenDuplicateCount > 0 ? `
      <div class="inline-note success-note">${escapeHtml(t(
        "knowledge.duplicates_hidden",
        { count: hiddenDuplicateCount },
      ))}</div>` : ""}
    ${items.map((item, index) =>
      renderKnowledgeCard(item, duplicateIndexes.has(index))).join("")}`;
}

function renderKnowledgeCard(item, isDuplicate) {
  const rawContent = safeText(item?.content);
  const title = knowledgeTitle(item);
  const body = cleanKnowledgeMarkdown(rawContent, title);
  const excerpt = truncateText(body || t("dynamic.no_snippet"), 320);
  const metadata = item?.metadata && typeof item.metadata === "object" ?
    item.metadata : {};
  const attributes = [
    ["knowledge.document", item?.document_id],
    ["knowledge.chunk", item?.chunk_id],
    ["knowledge.score", formatKnowledgeScore(item?.score)],
    ["knowledge.retrieval_mode", metadata.retrieval_mode],
    ["knowledge.rerank_provider", metadata.rerank_provider],
    ["knowledge.service", metadata.service],
    ["knowledge.dependency", metadata.dependency],
    ["knowledge.category", metadata.category],
  ].filter(([, value]) => value !== undefined && value !== null && value !== "");
  const paragraphs = knowledgeParagraphs(body);
  return `
    <article class="search-result knowledge-card">
      <div class="knowledge-card-heading">
        <div>
          <span class="knowledge-card-kicker">${escapeHtml(t("knowledge.summary"))}</span>
          <h4>${escapeHtml(title)}</h4>
        </div>
        ${isDuplicate ? `<span class="badge warning">${escapeHtml(t("knowledge.duplicate_seed"))}</span>` : ""}
      </div>
      <div class="knowledge-excerpt">${knowledgeParagraphs(excerpt)}</div>
      <div class="knowledge-attributes">
        ${attributes.map(([label, value]) => `
          <span><b>${escapeHtml(t(label))}</b><code>${escapeHtml(String(value))}</code></span>`).join("")}
      </div>
      ${Array.from(body).length > Array.from(excerpt).length ? `
        <details class="knowledge-expand">
          <summary>
            <span class="when-closed">${escapeHtml(t("knowledge.expand"))}</span>
            <span class="when-open">${escapeHtml(t("knowledge.collapse"))}</span>
          </summary>
          <div class="knowledge-full-text">${paragraphs}</div>
        </details>` : ""}
      <details class="knowledge-metadata">
        <summary>${escapeHtml(t("dynamic.retrieval_metadata"))}</summary>
        <div class="knowledge-metadata-grid">
          ${attributes.map(([label, value]) => `
            <div><span>${escapeHtml(t(label))}</span><code>${escapeHtml(String(value))}</code></div>`).join("")}
        </div>
        <pre>${escapeHtml(safeJson(metadata))}</pre>
      </details>
      <details class="knowledge-raw">
        <summary>${escapeHtml(t("knowledge.raw_content"))}</summary>
        <pre>${escapeHtml(rawContent || t("dynamic.no_snippet"))}</pre>
      </details>
    </article>`;
}

function knowledgeTitle(item) {
  const explicit = safeText(item?.title).trim();
  if (explicit) return explicit.replace(/^#+\s*/, "");
  const heading = safeText(item?.content).match(/^\s*#\s+(.+)$/m);
  return heading?.[1]?.trim() || t("dynamic.untitled_knowledge");
}

function cleanKnowledgeMarkdown(content, title) {
  const lines = safeText(content).replace(/\r\n/g, "\n").split("\n");
  let firstHeadingRemoved = false;
  return lines.map((line) => {
    if (!firstHeadingRemoved && /^#\s+/.test(line.trim()) &&
        line.trim().replace(/^#+\s*/, "").trim() === title) {
      firstHeadingRemoved = true;
      return "";
    }
    return line
      .replace(/^\s{0,3}#{1,6}\s+/, "")
      .replace(/^\s*[-*+]\s+/, "• ")
      .trimEnd();
  }).join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

function knowledgeParagraphs(content) {
  const paragraphs = safeText(content).split(/\n{2,}/)
    .map((paragraph) => paragraph.trim())
    .filter(Boolean);
  if (!paragraphs.length) return `<p>${escapeHtml(t("dynamic.no_snippet"))}</p>`;
  return paragraphs.map((paragraph) =>
    `<p>${escapeHtml(paragraph).replaceAll("\n", "<br>")}</p>`).join("");
}

function formatKnowledgeScore(value) {
  const score = safeNumber(value);
  return score === null ? "—" : score.toFixed(4).replace(/0+$/, "").replace(/\.$/, "");
}

function findDuplicateKnowledgeResults(items) {
  const duplicateIndexes = new Set();
  const signatures = new Map();
  items.forEach((item, index) => {
    const title = normalizeKnowledgeSignature(knowledgeTitle(item));
    const content = normalizeKnowledgeSignature(
      cleanKnowledgeMarkdown(item?.content, knowledgeTitle(item)),
    ).slice(0, 1000);
    const keys = [
      title && content ? `title_content:${title}:${content}` : "",
    ].filter(Boolean);
    keys.forEach((key) => {
      if (signatures.has(key)) {
        duplicateIndexes.add(signatures.get(key));
        duplicateIndexes.add(index);
      } else {
        signatures.set(key, index);
      }
    });
  });
  return duplicateIndexes;
}

function normalizeKnowledgeSignature(value) {
  return safeText(value).toLocaleLowerCase()
    .replace(/[\s\p{P}\p{S}]+/gu, "");
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
  const longTermCount = safeNumber(metadata.long_term_memory_count) ?? memoryEvidence.length;
  const memoryMatchMessage = longTermCount > 0 ?
    t("memory.long_term_loaded", { count: longTermCount }) :
    t("memory.no_long_term_match");
  byId("memory-context").className = "";
  byId("memory-context").innerHTML = `
    <div class="memory-status-grid">
      <article><span>${escapeHtml(t("dynamic.session_memory"))}</span><strong>${availabilityLabel(sessionAvailable)}</strong></article>
      <article><span>${escapeHtml(t("dynamic.long_term_memory"))}</span><strong>${availabilityLabel(longTermAvailable)}</strong></article>
    </div>
    <div class="inline-note ${longTermCount > 0 ? "success-note" : "info-note"}">${escapeHtml(memoryMatchMessage)}</div>
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
  const exchanges = buildHistoryExchanges(messages);
  byId("history-messages").innerHTML = exchanges.map((exchange) => `
    <article class="history-exchange">
      <div class="history-message-head">
        <strong>${escapeHtml(t("history.conversation"))}</strong>
        <time>${escapeHtml(formatHistoryTime(exchange.created_at))}</time>
      </div>
      <dl class="history-exchange-body">
        <div>
          <dt>${escapeHtml(t("history.user_message"))}</dt>
          <dd>${escapeHtml(exchange.user || t("dynamic.message_unavailable"))}</dd>
        </div>
        <div>
          <dt>${escapeHtml(t("history.assistant_summary"))}</dt>
          <dd>${escapeHtml(truncateText(exchange.assistant || t("dynamic.message_unavailable"), 360))}</dd>
        </div>
        <div>
          <dt>request_id</dt>
          <dd><code>${escapeHtml(exchange.request_id || "—")}</code></dd>
        </div>
      </dl>
      ${exchange.metadata ? `
        <details>
          <summary>${escapeHtml(t("dynamic.message_metadata"))}</summary>
          <pre>${escapeHtml(safeJson(exchange.metadata))}</pre>
        </details>` : ""}
    </article>`).join("");
}

function buildHistoryExchanges(messages) {
  const chronological = [...messages].sort((a, b) => {
    const at = parseHistoryDate(a?.created_at);
    const bt = parseHistoryDate(b?.created_at);
    if (at !== null && bt !== null && at !== bt) return at - bt;
    return 0;
  });
  const exchanges = [];
  let pendingUser = null;
  chronological.forEach((message) => {
    const role = safeText(message?.role).toLowerCase();
    if (role === "user") {
      pendingUser = message;
      return;
    }
    if (role === "assistant") {
      exchanges.push({
        user: pendingUser?.content || "",
        assistant: message?.content || "",
        created_at: message?.created_at || pendingUser?.created_at || "",
        request_id: message?.request_id || pendingUser?.request_id || "",
        metadata: message?.metadata || pendingUser?.metadata,
      });
      pendingUser = null;
      return;
    }
    exchanges.push({
      user: role || t("common.unknown"),
      assistant: message?.content || "",
      created_at: message?.created_at || "",
      request_id: message?.request_id || "",
      metadata: message?.metadata,
    });
  });
  if (pendingUser) {
    exchanges.push({
      user: pendingUser.content || "",
      assistant: "",
      created_at: pendingUser.created_at || "",
      request_id: pendingUser.request_id || "",
      metadata: pendingUser.metadata,
    });
  }
  const hasTimestamp = exchanges.some((exchange) => parseHistoryDate(exchange.created_at) !== null);
  return hasTimestamp ? exchanges.sort((a, b) =>
    (parseHistoryDate(b.created_at) ?? 0) - (parseHistoryDate(a.created_at) ?? 0)) :
    exchanges.reverse();
}

function parseHistoryDate(value) {
  if (!value) return null;
  const time = new Date(value).getTime();
  return Number.isNaN(time) ? null : time;
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
    let resultDetails = [];
    if (result?.run_id) {
      try {
        const details = await apiFetch(`/api/v1/eval/runs/${encodeURIComponent(result.run_id)}/results`);
        resultDetails = safeArray(details?.results);
      } catch {
        resultDetails = [];
      }
    }
    byId("eval-result").className = "";
    const total = Number(result?.total ?? 0);
    const failedReasons = resultDetails.flatMap((item) => safeArray(item?.failure_reasons))
      .filter(Boolean);
    byId("eval-result").innerHTML = total === 0 ? `
      <div class="empty-state compact"><p>${escapeHtml(t("eval.no_cases"))}</p></div>
      <div class="inline-note">${escapeHtml(t("dynamic.run_id"))}: ${escapeHtml(result?.run_id || t("dynamic.not_returned"))}</div>` : `
      <div class="memory-status-grid">
        <article><span>${escapeHtml(t("common.total"))}</span><strong>${escapeHtml(String(result?.total ?? 0))}</strong></article>
        <article><span>${escapeHtml(t("common.passed"))}</span><strong>${escapeHtml(String(result?.passed ?? 0))}</strong></article>
        <article><span>${escapeHtml(t("common.failed"))}</span><strong>${escapeHtml(String(result?.failed ?? 0))}</strong></article>
        <article><span>${escapeHtml(t("common.status"))}</span><strong>${escapeHtml(result?.status || t("common.unknown"))}</strong></article>
      </div>
      <div class="inline-note">${escapeHtml(t("eval.major_failure_reasons"))}: ${escapeHtml(failedReasons.slice(0, 4).join(" · ") || "—")}</div>
      <div class="inline-note">${escapeHtml(t("dynamic.run_id"))}: ${escapeHtml(result?.run_id || t("dynamic.not_returned"))}</div>
      ${resultDetails.length ? `<div class="eval-case-list">${resultDetails.map((item) => `
        <article class="eval-case-row ${item?.passed ? "passed" : "failed"}">
          <div><b>${escapeHtml(item?.case_id || t("common.unknown"))}</b><span>${escapeHtml(item?.passed ? t("common.passed") : t("common.failed"))}</span></div>
          <p>${escapeHtml(safeArray(item?.failure_reasons).join(" · ") || t("eval.no_failure_reasons"))}</p>
          <small>request_id: ${escapeHtml(item?.request_id || "—")} · trace_id: ${escapeHtml(item?.trace_id || "—")}</small>
        </article>`).join("")}</div>` : ""}`;
    renderToast(t("dynamic.eval_completed"), "success");
  } catch (error) {
    const message = String(error.message || "");
    const hint = message.includes("503") ?
      t("eval.mysql_unavailable") : t("dynamic.eval_unavailable", { message });
    byId("eval-result").innerHTML = polishedEmpty(t("dynamic.eval_unavailable", {
      message: error.message,
    })) + `<div class="inline-note danger-note">${escapeHtml(hint)}</div>`;
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
  updateTraceGuide();
  renderRuntimeStatuses();
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

function renderRuntimeStatuses() {
  const response = state.latestChatResponse;
  const metadata = response?.metadata || {};
  const runs = safeArray(response?.tool_runs);
  const hasResponse = Boolean(response);
  const requestFailed = state.lastRequestStatus === "error";

  setRuntimeStatus(
    "status-eino",
    requestFailed ? "error" : hasResponse ? "active" : "unknown",
    requestFailed ? t("status.error") :
      hasResponse ? t("status.graph_active") : t("status.unknown"),
  );

  const failedRun = runs.some((run) => run?.success === false);
  const fallbackRun = runs.some(toolRunUsedFallback) ||
    safeArray(response?.answer?.evidence).some((item) =>
      item?.metadata?.fallback_used === true);
  const toolState = requestFailed || failedRun ? "error" :
    fallbackRun ? "fallback" :
      runs.length ? "active" : hasResponse ? "available" : "unknown";
  const toolLabel = toolState === "error" ? t("status.error") :
    toolState === "fallback" ? t("status.fallback") :
      runs.length ? t("status.tools_active", { count: runs.length }) :
        hasResponse ? t("status.tools_available") : t("status.unknown");
  setRuntimeStatus("status-tools", toolState, toolLabel);

  const recentCount = safeNumber(metadata.recent_message_count);
  const redisKnown = metadata.session_memory_available !== undefined ||
    metadata.session_context_loaded !== undefined ||
    recentCount !== null;
  const redisState = metadata.session_memory_available === false ? "fallback" :
    redisKnown && recentCount > 0 ? "active" :
      redisKnown ? "available" : "unknown";
  const redisLabel = redisState === "fallback" ? t("status.redis_fallback") :
    redisState === "active" ? t("status.redis_active", { count: recentCount }) :
      redisState === "available" ? t("status.redis_available") :
        t("status.unknown");
  setRuntimeStatus("status-redis", redisState, redisLabel);

  const longTermCount = safeNumber(metadata.long_term_memory_count);
  const longTermUnavailable = responseHasLimitation(
    response,
    "LONG_TERM_MEMORY_UNAVAILABLE",
  ) || metadata.long_term_memory_available === false;
  const mysqlState = longTermUnavailable ? "fallback" :
    longTermCount > 0 ? "active" :
      longTermCount !== null ? "available" : "unknown";
  const mysqlLabel = mysqlState === "fallback" ? t("status.mysql_fallback") :
    mysqlState === "active" ? t("status.mysql_active", { count: longTermCount }) :
      mysqlState === "available" ? t("status.mysql_available") :
        t("status.unknown");
  setRuntimeStatus("status-mysql", mysqlState, mysqlLabel);

  const sseState = state.streamStatus === "completed" ? "active" :
    state.streamStatus === "failed" ? "error" :
      state.streamStatus === "running" ? "available" : "unknown";
  const sseLabel = sseState === "active" ? t("status.sse_completed") :
    sseState === "error" ? t("status.sse_error") :
      sseState === "available" ? t("status.sse_running") :
        t("status.not_run");
  setRuntimeStatus("status-sse", sseState, sseLabel);

  const agentMode = safeText(metadata.agent_mode);
  const fallbackUsed = metadata.fallback_used === true ||
    agentMode === "deterministic";
  const llmState = requestFailed ? "error" :
    fallbackUsed ? "fallback" :
      agentMode === "eino_react" ? "active" : "unknown";
  const llmLabel = llmState === "error" ? t("status.error") :
    llmState === "fallback" ? t("status.llm_fallback") :
      llmState === "active" ? t("status.llm_active") : t("status.unknown");
  setRuntimeStatus("status-llm", llmState, llmLabel);
}

function setRuntimeStatus(id, status, label) {
  const element = byId(id);
  if (!element) return;
  element.className = `status-pill ${status}`;
  const text = element.querySelector("small");
  if (text) text.textContent = label;
  element.title = `${t(`status.${status}`)} — ${label}`;
}

function toolRunUsedFallback(run) {
  if (run?.fallback_used === true || run?.metadata?.fallback_used === true) {
    return true;
  }
  const code = safeText(run?.error_code || run?.warning_code).toUpperCase();
  return code.includes("FALLBACK");
}

function responseHasLimitation(response, code) {
  return safeArray(response?.answer?.limitations).some((limitation) =>
    safeText(limitation?.code).toUpperCase() === code);
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
  if (value === null || value === undefined || value === "") return null;
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
  const runes = Array.from(text);
  if (runes.length <= limit) return text;
  return `${runes.slice(0, limit).join("").trimEnd()}…`;
}

function setText(id, value) {
  byId(id).textContent = value;
}
