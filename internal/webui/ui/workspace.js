(() => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  const samples = {
    simple: "Translate “good morning” into French.",
    standard: "Explain how a binary search tree works, with an example.",
    complex:
      "Design a distributed rate limiter with regional failover. Analyze consistency, clock skew, and partition recovery.",
  };
  let records = [],
    generation = 0,
    controller = null;
  const escape = (value) =>
    String(value ?? "").replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
  const ms = (value) =>
    Number(value) < 1 ? "<1 ms" : `${Number(value).toFixed(0)} ms`;
  const usd = (value) => `$${(Number(value || 0) / 1e9).toFixed(6)}`;
  const title = (value) =>
    String(value || "")
      .replace(/_/g, " ")
      .replace(/^./, (c) => c.toUpperCase());
  function busy(on) {
    $("runTrial").disabled = on;
    $("runTrial").textContent = on ? "Routing request…" : "Run request ↗";
    $("resultCard").setAttribute("aria-busy", String(on));
    for (const id of ["trialMessage", "trialRoute", "trialScenario"])
      $(id).disabled = on;
    document.querySelectorAll("[data-sample]").forEach((b) => {
      b.disabled = on;
    });
  }
  function render(result) {
    $("emptyResult").hidden = true;
    $("trialResult").hidden = false;
    $("outcomeLabel").textContent = result.id.slice(0, 12);
    $("resultRoute").textContent = `${title(result.routing.route)} route`;
    const classification = result.routing.classification;
    $("resultClassifier").textContent = classification
      ? `${title(classification.tier)} task · local rule classifier`
      : "Explicit selection · classifier bypassed";
    $("resultStatus").textContent = title(result.status);
    $("resultStatus").classList.toggle("error-pill", result.status === "error");
    $("resultLatency").textContent = ms(result.latency_ms);
    $("resultTokens").textContent = result.usage.total_tokens;
    $("resultCost").textContent = usd(result.cost_nano_usd);
    $("trialAttempts").innerHTML = result.attempts
      .map(
        (a) =>
          `<div class="attempt-row"><span class="attempt-index">${Number(a.number)}</span><div><strong>${escape(a.model)}</strong><small>${escape(a.provider)}</small></div><div><span class="attempt-state ${a.status === "error" ? "error" : ""}">${escape(a.error_code || "Completed")}</span><br><small>${ms(a.latency_ms)}</small></div></div>`,
      )
      .join("");
    $("decisionNote").textContent =
      result.status === "error"
        ? "All available targets failed. Relay stops within the configured attempt limit and returns a structured error."
        : result.fallback_count > 0
          ? `The primary provider failed. Relay recovered on the next target without changing the client request. ${result.fallback_count} fallback used.`
          : classification
            ? `The local classifier selected ${classification.tier} complexity. Your routing policy mapped it to the ${result.routing.route} route.`
            : `You selected the ${result.routing.route} route directly. Automatic classification was bypassed.`;
    $("trialJSON").textContent = JSON.stringify(result, null, 2);
    $("trialHistory").innerHTML = records
      .map(
        (r) =>
          `<tr><td>${escape(r.id.slice(0, 10))}</td><td>${escape(r.routing.route)}</td><td>${escape(title(r.status))}</td><td>${r.attempts.length}</td><td>${ms(r.latency_ms)}</td><td>${usd(r.cost_nano_usd)}</td></tr>`,
      )
      .join("");
    $("emptyHistory").hidden = records.length > 0;
  }
  document.querySelectorAll("[data-sample]").forEach((button) =>
    button.addEventListener("click", () => {
      $("trialMessage").value = samples[button.dataset.sample];
      document
        .querySelectorAll("[data-sample]")
        .forEach((b) => b.setAttribute("aria-pressed", String(b === button)));
    }),
  );
  $("trialMessage").addEventListener("input", () =>
    document
      .querySelectorAll("[data-sample]")
      .forEach((b) => b.setAttribute("aria-pressed", "false")),
  );
  $("trialForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    const version = ++generation;
    controller?.abort();
    controller = new AbortController();
    busy(true);
    $("trialError").hidden = true;
    $("trialResult").hidden = true;
    $("emptyResult").hidden = false;
    $("outcomeLabel").textContent = "REQUEST IN FLIGHT";
    try {
      const response = await fetch("/v1/try", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        credentials: "omit",
        redirect: "error",
        signal: controller.signal,
        body: JSON.stringify({
          message: $("trialMessage").value,
          route: $("trialRoute").value,
          scenario: $("trialScenario").value,
        }),
      });
      const result = await response.json();
      if (version !== generation) return;
      if (!result.routing || !Array.isArray(result.attempts))
        throw new Error(result.error?.code || "request_failed");
      records = [result, ...records].slice(0, 20);
      render(result);
    } catch (error) {
      if (version !== generation) return;
      $("trialError").textContent = title(
        error.message || "Unable to reach the gateway. Please retry.",
      );
      $("trialError").hidden = false;
      $("outcomeLabel").textContent = "REQUEST FAILED";
    } finally {
      if (version === generation) busy(false);
    }
  });
  $("clearTrial").addEventListener("click", () => {
    generation++;
    controller?.abort();
    records = [];
    busy(false);
    for (const id of ["trialHistory", "trialAttempts", "trialJSON"])
      $(id).replaceChildren();
    $("trialMessage").value = "";
    $("trialResult").hidden = true;
    $("emptyResult").hidden = false;
    $("emptyHistory").hidden = false;
    $("trialError").hidden = true;
    $("outcomeLabel").textContent = "AWAITING REQUEST";
  });
  if (new URLSearchParams(location.search).get("scenario") === "fallback")
    $("trialScenario").value = "rate_limit";
  fetch("/health", { credentials: "omit", redirect: "error" })
    .then((r) => {
      if (!r.ok) throw new Error();
      $("workspaceHealth").textContent = "● Gateway online";
    })
    .catch(() => {
      $("workspaceHealth").textContent = "Gateway unavailable";
    });
})();
