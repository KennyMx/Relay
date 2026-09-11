(() => {
  "use strict";

  const state = {
    key: sessionStorage.getItem("relay_api_key") || "",
    keyId: sessionStorage.getItem("relay_key_id") || "",
    routes: [],
    requests: [],
  };

  const $ = (id) => document.getElementById(id);
  const accessView = $("accessView");
  const consoleView = $("consoleView");
  const accessError = $("accessError");
  const chatError = $("chatError");
  const historyError = $("historyError");

  function show(element, visible = true) { element.classList.toggle("hidden", !visible); }
  function setBusy(button, busy, label) {
    button.disabled = busy;
    if (!button.dataset.label) button.dataset.label = button.innerHTML;
    button.innerHTML = busy ? label : button.dataset.label;
  }
  function errorMessage(error) {
    return error && error.message ? error.message : "Something went wrong. Check the gateway and try again.";
  }
  function displayError(element, error) {
    element.textContent = errorMessage(error);
    show(element);
  }
  function clearError(element) { element.textContent = ""; show(element, false); }
  function toast(message) {
    const element = $("toast");
    element.textContent = message; show(element);
    window.clearTimeout(toast.timer);
    toast.timer = window.setTimeout(() => show(element, false), 2600);
  }
  function escapeHTML(value) {
    return String(value ?? "").replace(/[&<>'"]/g, (character) => ({"&":"&amp;","<":"&lt;",">":"&gt;","'":"&#39;",'"':"&quot;"})[character]);
  }
  function shortID(value) { return value ? `${value.slice(0, 8)}…${value.slice(-4)}` : "—"; }
  function number(value) { return new Intl.NumberFormat().format(value || 0); }
  function dollars(nanoUSD) {
    const amount = (nanoUSD || 0) / 1e9;
    if (amount === 0) return "$0.00";
    if (amount < .01) return `$${amount.toFixed(6)}`;
    return new Intl.NumberFormat(undefined, {style: "currency", currency: "USD", maximumFractionDigits: 4}).format(amount);
  }
  function duration(ms) { return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(2)} s`; }
  function dateTime(value) {
    if (!value) return "—";
    return new Intl.DateTimeFormat(undefined, {month:"short", day:"numeric", hour:"2-digit", minute:"2-digit", second:"2-digit"}).format(new Date(value));
  }

  async function api(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (options.token) headers.set("Authorization", `Bearer ${options.token}`);
    if (options.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
    const response = await fetch(path, {...options, headers});
    if (response.status === 204) return null;
    let payload;
    try { payload = await response.json(); } catch { payload = null; }
    if (!response.ok) {
      const code = payload?.error?.code;
      if (response.status === 401 && options.token === state.key) disconnect(false);
      throw new Error(code ? code.replaceAll("_", " ") : `Request failed with HTTP ${response.status}`);
    }
    return payload;
  }

  async function checkHealth() {
    const badge = $("healthBadge");
    try {
      await api("/health");
      badge.className = "status-pill status-live";
      badge.innerHTML = '<span class="status-dot"></span>Gateway online';
    } catch {
      badge.className = "status-pill status-down";
      badge.innerHTML = '<span class="status-dot"></span>Gateway unavailable';
    }
  }

  function switchAccess(mode) {
    const connect = mode === "connect";
    $("connectTab").classList.toggle("active", connect);
    $("connectTab").setAttribute("aria-selected", String(connect));
    $("createTab").classList.toggle("active", !connect);
    $("createTab").setAttribute("aria-selected", String(!connect));
    show($("connectForm"), connect); show($("createForm"), !connect); show($("createdKeyPanel"), false);
    clearError(accessError);
  }

  function persistSession(key, id = "") {
    state.key = key; state.keyId = id;
    sessionStorage.setItem("relay_api_key", key);
    if (id) sessionStorage.setItem("relay_key_id", id);
  }

  async function connect(key, id = "") {
    clearError(accessError);
    persistSession(key.trim(), id);
    try {
      await loadConsole();
      show(accessView, false); show(consoleView); show($("disconnectButton"));
    } catch (error) {
      disconnect(false);
      displayError(accessError, error);
      throw error;
    }
  }

  function disconnect(withMessage = true) {
    state.key = ""; state.keyId = ""; state.routes = []; state.requests = [];
    sessionStorage.removeItem("relay_api_key"); sessionStorage.removeItem("relay_key_id");
    show(consoleView, false); show(accessView); show($("disconnectButton"), false);
    $("apiKeyInput").value = "";
    switchAccess("connect");
    if (withMessage) toast("Disconnected from Relay");
  }

  async function createKey(event) {
    event.preventDefault(); clearError(accessError);
    const button = event.submitter;
    setBusy(button, true, "Creating…");
    try {
      const result = await api("/v1/keys", {
        method: "POST",
        token: $("adminTokenInput").value,
        body: JSON.stringify({name: $("keyNameInput").value, requests_per_minute: Number($("rpmInput").value), burst: Number($("burstInput").value)}),
      });
      state.pendingKey = result.api_key; state.pendingKeyId = result.id;
      $("createdKeyValue").textContent = result.api_key;
      show($("createForm"), false); show($("createdKeyPanel"));
      $("adminTokenInput").value = "";
    } catch (error) { displayError(accessError, error); }
    finally { setBusy(button, false); }
  }

  async function copyCreatedKey() {
    try { await navigator.clipboard.writeText(state.pendingKey); toast("API key copied"); }
    catch { toast("Select the key and copy it manually"); }
  }

  async function loadConsole() {
    const [routeData, requestData] = await Promise.all([
      api("/v1/routes", {token: state.key}),
      api("/v1/requests?limit=100&offset=0", {token: state.key}),
    ]);
    state.routes = routeData.routes || [];
    if (routeData.key?.id) {
      state.keyId = routeData.key.id;
      sessionStorage.setItem("relay_key_id", routeData.key.id);
    }
    state.requests = requestData.data || [];
    renderRoutes(routeData.default_route);
    renderRequests(); renderMetrics();
    $("baseUrlLabel").textContent = `${location.origin}/v1/chat/completions`;
    const keyName = routeData.key?.name ? `${routeData.key.name} · ` : "";
    $("welcomeLine").textContent = `${keyName}${state.routes.length} route${state.routes.length === 1 ? "" : "s"} available · ${routeData.key?.requests_per_minute || "—"} requests/min`;
  }

  function renderRoutes(defaultRoute) {
    const select = $("routeSelect");
    const previous = select.value;
    select.innerHTML = state.routes.map((route) => `<option value="${escapeHTML(route.name)}" ${route.name === defaultRoute ? "selected" : ""}>${escapeHTML(route.name)}${route.name === defaultRoute ? " · default" : ""}</option>`).join("");
    if (state.routes.some((route) => route.name === previous)) select.value = previous;
    renderRouteFlow();
  }

  function selectedRoute() { return state.routes.find((route) => route.name === $("routeSelect").value); }
  function renderRouteFlow() {
    const route = selectedRoute();
    const providerSelect = $("providerSelect");
    providerSelect.innerHTML = '<option value="">Route default</option>';
    if (!route) { $("routeFlow").innerHTML = ""; return; }
    for (const target of route.targets) {
      const option = document.createElement("option"); option.value = target.provider; option.textContent = target.provider; providerSelect.appendChild(option);
    }
    $("routeFlow").innerHTML = route.targets.map((target, index) => `
      <div class="route-target">
        <span class="route-index">${String(index + 1).padStart(2, "0")}</span>
        <div><h3>${escapeHTML(target.provider)}</h3><p>${escapeHTML(target.model)}</p><em>${index === 0 ? "Primary target" : `Fallback ${index}`}</em></div>
      </div>`).join("");
  }

  function renderMetrics() {
    const requests = state.requests;
    const successes = requests.filter((request) => request.status === "success").length;
    const totalTokens = requests.reduce((sum, request) => sum + (request.usage?.total_tokens || 0), 0);
    const totalCost = requests.reduce((sum, request) => sum + (request.cost_nano_usd || 0), 0);
    $("requestMetric").textContent = number(requests.length);
    $("successMetric").textContent = requests.length ? `${Math.round(successes / requests.length * 100)}%` : "—";
    $("successCaption").textContent = requests.length ? `${successes} of ${requests.length} completed` : "Awaiting traffic";
    $("tokenMetric").textContent = number(totalTokens);
    $("costMetric").textContent = dollars(totalCost);
  }

  function renderRequests() {
    const rows = $("requestRows");
    show($("emptyState"), state.requests.length === 0);
    rows.innerHTML = state.requests.map((request) => `
      <tr>
        <td><span class="request-id" title="${escapeHTML(request.id)}">${escapeHTML(shortID(request.id))}</span><span class="request-time">${escapeHTML(dateTime(request.created_at))}</span></td>
        <td class="provider-cell"><b>${escapeHTML(request.provider || "pending")}</b><small>${escapeHTML(request.model || request.route)}</small></td>
        <td><span class="table-status ${request.status === "error" ? "error" : ""}">${escapeHTML(request.status)}</span></td>
        <td>${number(request.usage?.total_tokens)}</td>
        <td>${escapeHTML(duration(request.latency_ms || 0))}</td>
        <td>${escapeHTML(dollars(request.cost_nano_usd))}</td>
        <td>${number(request.fallback_count)}</td>
        <td><button class="view-button" type="button" data-request-id="${escapeHTML(request.id)}">Inspect →</button></td>
      </tr>`).join("");
  }

  async function sendChat(event) {
    event.preventDefault(); clearError(chatError);
    const button = $("sendButton"); setBusy(button, true, "Routing…");
    const messages = [];
    const system = $("systemInput").value.trim();
    if (system) messages.push({role: "system", content: system});
    messages.push({role: "user", content: $("promptInput").value.trim()});
    const body = {model: $("routeSelect").value, messages, max_tokens: Number($("maxTokensInput").value)};
    if ($("providerSelect").value) body.provider = $("providerSelect").value;
    try {
      const result = await api("/v1/chat/completions", {method: "POST", token: state.key, body: JSON.stringify(body)});
      $("completionProvider").textContent = `${result.provider} · ${result.model}`;
      $("completionMeta").textContent = shortID(result.id);
      $("completionText").textContent = result.choices?.[0]?.message?.content || "No text response";
      $("completionStats").innerHTML = `<span><b>${number(result.usage?.total_tokens)}</b> tokens</span><span><b>${duration(result.latency_ms)}</b> latency</span><span><b>${dollars(result.cost_nano_usd)}</b> estimated</span><span><b>${number(result.fallback_count)}</b> fallbacks</span><span>${result.usage?.simulated ? "Simulated usage" : "Provider usage"}</span>`;
      show($("completionPanel"));
      await refreshHistory();
    } catch (error) { displayError(chatError, error); await refreshHistory().catch(() => {}); }
    finally { setBusy(button, false); }
  }

  async function refreshHistory() {
    clearError(historyError);
    const data = await api("/v1/requests?limit=100&offset=0", {token: state.key});
    state.requests = data.data || []; renderRequests(); renderMetrics();
  }

  async function inspectRequest(id) {
    try {
      const request = await api(`/v1/requests/${encodeURIComponent(id)}`, {token: state.key});
      $("dialogTitle").textContent = shortID(request.id);
      const attempts = request.attempts || [];
      $("requestDetail").innerHTML = `
        <div class="detail-grid">
          <div class="detail-stat"><span>Status</span><b>${escapeHTML(request.status)}</b></div>
          <div class="detail-stat"><span>Total tokens</span><b>${number(request.usage?.total_tokens)}</b></div>
          <div class="detail-stat"><span>Latency</span><b>${escapeHTML(duration(request.latency_ms || 0))}</b></div>
          <div class="detail-stat"><span>Est. cost</span><b>${escapeHTML(dollars(request.cost_nano_usd))}</b></div>
        </div>
        <h3 class="attempt-title">Provider attempts · ${attempts.length}</h3>
        ${attempts.map((attempt) => `<div class="attempt">
          <span class="attempt-number">${attempt.number}</span>
          <div><b>${escapeHTML(attempt.provider)}</b><small>${escapeHTML(attempt.model)} · ${dateTime(attempt.started_at)}</small></div>
          <div class="attempt-result"><span class="table-status ${attempt.status === "error" ? "error" : ""}">${escapeHTML(attempt.status)}</span><small>${escapeHTML(attempt.error_code || `${number(attempt.usage?.total_tokens)} tokens · ${duration(attempt.latency_ms || 0)}`)}</small></div>
        </div>`).join("")}`;
      $("requestDialog").showModal();
    } catch (error) { toast(errorMessage(error)); }
  }

  function openRevokeDialog() {
    clearError($("revokeError"));
    $("revokeAdminInput").value = "";
    $("revokeDialog").showModal();
    $("revokeAdminInput").focus();
  }

  async function revokeCurrentKey(event) {
    event.preventDefault();
    clearError($("revokeError"));
    if (!state.keyId) { displayError($("revokeError"), new Error("Key ID is unavailable for this session")); return; }
    const button = $("confirmRevokeButton");
    setBusy(button, true, "Revoking…");
    try {
      await api(`/v1/keys/${encodeURIComponent(state.keyId)}`, {method: "DELETE", token: $("revokeAdminInput").value});
      $("revokeDialog").close();
      disconnect(false); toast("API key revoked");
    } catch (error) { displayError($("revokeError"), error); }
    finally { setBusy(button, false); }
  }

  $("connectTab").addEventListener("click", () => switchAccess("connect"));
  $("createTab").addEventListener("click", () => switchAccess("create"));
  $("connectForm").addEventListener("submit", async (event) => {
    event.preventDefault(); const button = event.submitter; setBusy(button, true, "Connecting…");
    try { await connect($("apiKeyInput").value); } catch {} finally { setBusy(button, false); }
  });
  $("createForm").addEventListener("submit", createKey);
  $("copyKeyButton").addEventListener("click", copyCreatedKey);
  $("useKeyButton").addEventListener("click", async () => { try { await connect(state.pendingKey, state.pendingKeyId); } catch {} });
  $("disconnectButton").addEventListener("click", () => disconnect());
  $("refreshButton").addEventListener("click", async () => {
    const icon = $("refreshButton").querySelector("span"); icon.classList.add("spin");
    try { await loadConsole(); toast("Console refreshed"); } catch (error) { displayError(historyError, error); }
    finally { icon.classList.remove("spin"); }
  });
  $("routeSelect").addEventListener("change", renderRouteFlow);
  $("chatForm").addEventListener("submit", sendChat);
  $("requestRows").addEventListener("click", (event) => { const button = event.target.closest("[data-request-id]"); if (button) inspectRequest(button.dataset.requestId); });
  $("closeDialog").addEventListener("click", () => $("requestDialog").close());
  $("requestDialog").addEventListener("click", (event) => { if (event.target === $("requestDialog")) $("requestDialog").close(); });
  $("revokeButton").addEventListener("click", openRevokeDialog);
  $("revokeForm").addEventListener("submit", revokeCurrentKey);
  document.querySelectorAll("[data-close-revoke]").forEach((button) => button.addEventListener("click", () => $("revokeDialog").close()));
  document.querySelectorAll("[data-reveal]").forEach((button) => button.addEventListener("click", () => {
    const input = $(button.dataset.reveal); const reveal = input.type === "password"; input.type = reveal ? "text" : "password"; button.textContent = reveal ? "Hide" : "Show";
  }));

  checkHealth(); window.setInterval(checkHealth, 30000);
  if (state.key) connect(state.key, state.keyId).catch(() => {});
})();
