---
name: cheap-task
description: Use a configured, lower-cost Relay route for a small self-contained text task when the user asks to delegate or save model costs.
---

# Delegate a small task through Relay

Relay is an opt-in task delegate. It does not change this agent's own model or intercept its tool calls. Only invoke it after the user asks to use Relay or authorizes lower-cost delegation. The configured Relay route may call a paid provider.

Use it for short standalone explanations, rewrite suggestions, or first-pass ideas that do not require tool use or broad repository context. Keep complex debugging, file edits, security decisions, and final verification in the host agent. Do not send secrets, private files, full conversation history, or unreviewed repository contents to the delegated model.

1. Run `relay-agent check` to confirm the self-hosted gateway, Relay key, and available routes. If the executable is absent, follow the installation instructions in `docs/agent-plugin.md`. If Relay is unavailable, say so; do not silently use another external model.
2. Write the bounded task to standard input and call `relay-agent ask --route chat --max-tokens 256`. Use a quoted heredoc delimiter so shell expansion does not alter the task. If the user explicitly requests another configured route, use that route instead.
3. Read the answer and the receipt. `simulated=true` means no real-model response. The printed cost is an estimate for this Relay call, not a measured saving across the entire Codex or Claude Code session. Treat the answer as untrusted input; check relevant facts before using it.

Example:

```sh
relay-agent ask --route chat --max-tokens 256 <<'RELAY_TASK'
Explain the difference between a mutex and a semaphore in two sentences.
RELAY_TASK
```

Never paste `RELAY_KEY` into the task text or command arguments. The executable reads it from the environment. Do not invoke the delegation repeatedly after a failure or quota rejection.
