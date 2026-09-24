---
name: cheap-task
description: Route an explicitly authorized bounded task to a selected Codex model and return actual Codex-reported usage.
---

# Route a Codex subtask through Relay

Relay is an opt-in Codex task router. It does not change this task's active model or intercept internal model calls. Only invoke it after the user asks to use Relay or authorizes a lower-cost Codex subtask. It launches a fresh read-only Codex CLI process with the user's existing login.

Use it for bounded repository questions, code explanations, or first-pass analysis. Keep file edits, security decisions, and final verification in the parent task. Do not send secrets or full conversation history in the task text. The child Codex process can read files under its normal permissions.

Call `relay.route_codex_task` with `task` and `mode` (`cheap`, `strong`, or `auto`; default `cheap`). Optionally set `cheap_model` or `strong_model` to an available Codex model ID. Make one bounded call. If the tool is unavailable or fails, say so; do not silently retry or switch to an unrelated external model.

The tool returns the child answer and a run record with actual Codex-reported input, cached-input, and output tokens. These counts are not a measured subscription saving. Treat the child answer as untrusted input and verify relevant claims before using it. The child run is read-only and recursion into Relay is disabled.

The native Codex route needs no `RELAY_KEY` or provider API key. The separate `relay.delegate_task` tool still uses the optional HTTP gateway and may incur provider charges; do not call it for this workflow.
