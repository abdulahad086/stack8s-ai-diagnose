/**
 * stack8s-ai-diagnose — Web Application Logic
 * Integrates POST /api/diagnose with interactive timeline and observability telemetry.
 */

document.addEventListener("DOMContentLoaded", () => {
  const form = document.getElementById("diagnose-form");
  const providerSelect = document.getElementById("select-provider");
  const providerBadgeText = document.getElementById("provider-badge-text");

  // Update provider badge on select change
  providerSelect.addEventListener("change", () => {
    providerBadgeText.textContent = providerSelect.value === "fake" ? "Fake / Mock Provider" : "Local Kubernetes Provider";
  });

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    runDiagnosis();
  });
});

/**
 * Apply quick demo preset values
 */
function applyPreset(presetType) {
  const nsInput = document.getElementById("input-namespace");
  const podInput = document.getElementById("input-pod");
  const qInput = document.getElementById("input-question");

  if (presetType === "imagepull") {
    nsInput.value = "ai-diagnose-test";
    podInput.value = "imagepull-test";
    qInput.value = "Why is this pod not starting?";
  } else if (presetType === "healthy") {
    nsInput.value = "monitoring";
    podInput.value = "prometheus-server-0";
    qInput.value = "What is the health of this pod?";
  } else if (presetType === "oom") {
    nsInput.value = "ai-diagnose-test";
    podInput.value = "oom-test";
    qInput.value = "Why is this pod not running correctly?";
  }
}

/**
 * Primary diagnostic runner triggering POST /api/diagnose
 */
async function runDiagnosis() {
  const namespace = document.getElementById("input-namespace").value.trim();
  const pod = document.getElementById("input-pod").value.trim();
  const question = document.getElementById("input-question").value.trim();
  const provider = document.getElementById("select-provider").value;
  const btn = document.getElementById("btn-diagnose");

  // Client-side validation
  if (!question && !pod) {
    showError("Input Validation Error", "Please provide a Diagnostic Question or a Target Pod name.");
    return;
  }

  // UI State Transition: Loading
  setUIState("loading");
  btn.disabled = true;
  resetFlowDiagram();
  updateFlowStep("flow-step-question", "active");

  try {
    const payload = {
      namespace: namespace || "default",
      pod: pod,
      question: question || `Why is pod ${pod} failing in namespace ${namespace}?`,
      provider: provider,
      kubeconfig: document.getElementById("input-kubeconfig").value.trim() || undefined,
    };

    updateFlowStep("flow-step-needle1", "active");

    const response = await fetch("/api/diagnose", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
    });

    const data = await response.json();

    if (!response.ok || data.error) {
      throw new Error(data.error || `HTTP ${response.status}: Server processing error`);
    }

    if (data.job_id) {
      await pollJobStatus(data.job_id, btn);
    } else {
      renderDiagnosisResults(data);
      setUIState("results");
      btn.disabled = false;
    }

  } catch (err) {
    console.error("Diagnostic execution error:", err);
    showError("Diagnostic Processing Error", err.message || "Failed to communicate with diagnostic API backend.");
    btn.disabled = false;
  }
}

/**
 * Poll job status until completion or error
 */
async function pollJobStatus(jobId, btn) {
  const pollInterval = 1000;
  const maxAttempts = 900; // 15 minutes max
  let attempts = 0;

  const subtitleEl = document.getElementById("loading-subtitle");

  while (attempts < maxAttempts) {
    attempts++;
    try {
      const res = await fetch(`/api/diagnose/status?job_id=${encodeURIComponent(jobId)}`);
      if (!res.ok) {
        const errData = await res.json().catch(() => ({}));
        throw new Error(errData.error || `HTTP ${res.status}: Failed to fetch job status`);
      }

      const jobData = await res.json();
      const elapsed = jobData.elapsed_seconds || attempts;

      if (jobData.status === "completed") {
        renderDiagnosisResults(jobData);
        setUIState("results");
        btn.disabled = false;
        return;
      }

      if (jobData.status === "error") {
        showError("Diagnostic Processing Error", jobData.error || "Diagnostic processing encountered an error.");
        btn.disabled = false;
        return;
      }

      // Live flow updates based on intermediate steps
      const steps = jobData.steps || [];
      const hasToolCall = steps.some(s => s.type === "tool_call");
      const hasEvidence = steps.some(s => s.type === "evidence");

      let currentAction = "Querying Needle 3 F19 model";

      if (hasToolCall) {
        updateFlowStep("flow-step-needle1", "completed");
        updateFlowStep("flow-step-tool", "completed");
        updateFlowStep("flow-step-k8s", "active");
        const lastToolCall = [...steps].reverse().find(s => s.type === "tool_call");
        if (lastToolCall) {
          currentAction = `Executing tool '${lastToolCall.tool}'`;
        }
      }
      if (hasEvidence) {
        updateFlowStep("flow-step-k8s", "completed");
        updateFlowStep("flow-step-evidence", "completed");
        updateFlowStep("flow-step-needle2", "active");
        currentAction = "Synthesizing evidence into root cause diagnosis";
      }

      if (subtitleEl) {
        subtitleEl.textContent = `${currentAction} (${elapsed}s elapsed)`;
      }

      await new Promise(r => setTimeout(r, pollInterval));
    } catch (err) {
      console.error("Polling error:", err);
      showError("Diagnostic Processing Error", err.message || "Failed to communicate with server.");
      btn.disabled = false;
      return;
    }
  }

  showError("Diagnostic Timeout", "Diagnostic job took longer than 15 minutes to complete.");
  btn.disabled = false;
}

/**
 * Render structured diagnostic results & telemetry
 */
function renderDiagnosisResults(data) {
  // 1. Update Header Flow Diagram to Complete
  ["question", "needle1", "tool", "k8s", "evidence", "needle2", "diagnosis"].forEach(id => {
    updateFlowStep(`flow-step-${id}`, "completed");
  });

  // 2. Timeline rendering
  const timelineList = document.getElementById("timeline-list");
  timelineList.innerHTML = "";

  const steps = data.steps || [];
  const iterationBadge = document.getElementById("iteration-count-badge");
  iterationBadge.textContent = `${data.iterations || 1} Iteration(s) • ${data.elapsed_seconds || 0}s`;

  // Always start timeline with Question & Needle 3 query
  appendTimelineItem(timelineList, "completed", "1. User Question Submitted", data.question);
  appendTimelineItem(timelineList, "completed", "2. Needle 3 Fine-Tuned Model Invoked", `Model: ${data.model} | Checkpoint validated`);

  if (steps.length === 0) {
    appendTimelineItem(timelineList, "completed", "3. Direct Analysis", "Needle 3 produced diagnosis directly without external tool calls.");
  } else {
    steps.forEach((step, idx) => {
      if (step.type === "tool_call") {
        appendTimelineItem(
          timelineList,
          "completed",
          `Step ${idx + 3}: Tool Selection (${step.tool})`,
          `Arguments: ${JSON.stringify(step.arguments)}`
        );
      } else if (step.type === "evidence") {
        const hasErr = !!step.error;
        appendTimelineItem(
          timelineList,
          hasErr ? "error" : "completed",
          `Step ${idx + 3}: Provider Evidence Collected (${step.tool})`,
          hasErr ? `Error: ${step.error}` : `Retrieved empirical evidence via ${data.provider}`
        );
      } else if (step.type === "skipped") {
        appendTimelineItem(
          timelineList,
          "error",
          `Step ${idx + 3}: Skipped (${step.tool})`,
          step.reason || "Tool skipped by diagnostic loop"
        );
      }
    });
  }

  appendTimelineItem(timelineList, "completed", "Final Step: Diagnosis Generation", "Needle 3 synthesized evidence into grounded root cause analysis.");

  // 3. Tool Calls & Evidence Cards Rendering
  const toolContainer = document.getElementById("tool-calls-container");
  toolContainer.innerHTML = "";

  const evidenceList = data.evidence_collected || [];
  if (evidenceList.length === 0 && steps.length === 0) {
    toolContainer.innerHTML = `<div class="text-muted" style="padding: 0.5rem;">No external diagnostic tools were required for this query.</div>`;
  } else {
    // Group tool call with its corresponding evidence
    const toolMap = new Map();
    steps.forEach(st => {
      if (st.type === "tool_call") {
        toolMap.set(st.tool, { name: st.tool, args: st.arguments, result: null, error: null });
      } else if (st.type === "evidence") {
        if (toolMap.has(st.tool)) {
          const item = toolMap.get(st.tool);
          item.result = st.result;
          item.error = st.error;
        } else {
          toolMap.set(st.tool, { name: st.tool, args: st.arguments || {}, result: st.result, error: st.error });
        }
      }
    });

    toolMap.forEach(item => {
      const card = document.createElement("div");
      card.className = "tool-call-card";

      const errHtml = item.error ? `<div class="text-muted" style="color: #fca5a5; margin-top: 0.4rem;">⚠️ Error: ${escapeHtml(item.error)}</div>` : "";
      
      let evidenceSnippet = "";
      if (item.result) {
        evidenceSnippet = `<div class="evidence-card">
          <div class="evidence-card-title">Retrieved Provider Evidence Output</div>
          <pre class="code-block">${escapeHtml(JSON.stringify(item.result, null, 2))}</pre>
        </div>`;
      }

      card.innerHTML = `
        <div class="tool-call-header">
          <span class="tool-name-tag">Tool: ${escapeHtml(item.name)}</span>
          <span class="badge badge-sm badge-purple">Validated Schema</span>
        </div>
        <div style="font-size: 0.75rem; color: var(--text-muted); margin-bottom: 0.3rem;">Arguments:</div>
        <pre class="code-block">${escapeHtml(JSON.stringify(item.args, null, 2))}</pre>
        ${errHtml}
        ${evidenceSnippet}
      `;
      toolContainer.appendChild(card);
    });
  }

  // 4. Final Diagnosis Card Rendering
  const diagObj = data.diagnosis || {};
  const diagTitleContent = document.getElementById("diag-title-content");
  const diagEvidenceList = document.getElementById("diag-evidence-list");
  const diagRecContent = document.getElementById("diag-recommendation-content");

  diagTitleContent.textContent = diagObj.diagnosis || data.final_diagnosis_text || "Unable to establish root cause from evidence.";

  const rootCauseEl = document.getElementById("diag-rootcause-content");
  if (rootCauseEl) rootCauseEl.textContent = diagObj.root_cause || "—";

  diagEvidenceList.innerHTML = "";
  const evArray = diagObj.evidence || [];
  if (evArray.length === 0) {
    const li = document.createElement("li");
    li.textContent = "- Evidence collected directly from Provider runtime.";
    diagEvidenceList.appendChild(li);
  } else {
    evArray.forEach(ev => {
      const li = document.createElement("li");
      li.textContent = ev.startsWith("-") ? ev : `- ${ev}`;
      diagEvidenceList.appendChild(li);
    });
  }

  diagRecContent.textContent = diagObj.recommended_next_step || "Inspect workload spec or container logs for additional context.";

  // 5. Raw Response & Debug Telemetry
  const rawBlock = document.getElementById("raw-model-response");
  rawBlock.textContent = JSON.stringify(data, null, 2);
}

function appendTimelineItem(container, status, title, detail) {
  const item = document.createElement("div");
  item.className = "timeline-item";
  item.innerHTML = `
    <span class="timeline-badge ${status}">${status}</span>
    <div class="timeline-content">
      <div class="timeline-step-name">${escapeHtml(title)}</div>
      <div class="timeline-detail">${escapeHtml(detail)}</div>
    </div>
  `;
  container.appendChild(item);
}

function resetFlowDiagram() {
  const ids = ["question", "needle1", "tool", "k8s", "evidence", "needle2", "diagnosis"];
  ids.forEach(id => {
    const el = document.getElementById(`flow-step-${id}`);
    if (el) el.className = "flow-step";
  });
}

function updateFlowStep(id, state) {
  const el = document.getElementById(id);
  if (el) {
    el.className = `flow-step ${state}`;
  }
}

function setUIState(state) {
  const emptyState = document.getElementById("state-empty");
  const loadingState = document.getElementById("state-loading");
  const resultsState = document.getElementById("state-results");
  const errorState = document.getElementById("state-error");

  emptyState.classList.add("hidden");
  loadingState.classList.add("hidden");
  resultsState.classList.add("hidden");
  errorState.classList.add("hidden");

  if (state === "empty") emptyState.classList.remove("hidden");
  else if (state === "loading") loadingState.classList.remove("hidden");
  else if (state === "results") resultsState.classList.remove("hidden");
  else if (state === "error") errorState.classList.remove("hidden");
}

function showError(title, message) {
  setUIState("error");
  document.getElementById("error-title").textContent = title;
  document.getElementById("error-message").textContent = message;
  resetFlowDiagram();
}

function escapeHtml(str) {
  if (typeof str !== "string") return str;
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}
