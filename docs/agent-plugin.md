# Relay for Codex

[Back to Relay](../README.md) · [Plugin files](../plugins/relay)

The Relay Codex plugin exposes `route_codex_task`: an MCP tool that launches one bounded, read-only native Codex CLI subtask on a selected model. It uses your existing Codex login and returns the answer plus Codex-reported input, cached-input, and output tokens. It does not change the parent task's active model.

## Install

From the repository root, install the executable and ensure it is on the `PATH` used by Codex:

```sh
go install ./cmd/relay-agent
export PATH="$(go env GOPATH)/bin:$PATH"
```

The repository has a [local plugin marketplace](../.agents/plugins/marketplace.json). Open it as a trusted Codex project, install Relay, and restart Codex so it loads `plugins/relay/.mcp.json`. Ask Codex to use `relay:cheap-task` for a bounded read-only question. The skill calls `relay.route_codex_task`. Use `cheap`, `strong`, or `auto`; the default is `cheap`. The child run writes only metadata to `.relay/runs.jsonl` in the current workspace. Open `relay-agent serve` locally to inspect it.

The child process inherits Codex authentication, runs with Codex's read-only sandbox, and is marked to prevent another Relay child from recursively routing again. Do not put secrets or full conversation history in the task argument. A nested agent has its own startup/context overhead, so use the plugin for tasks that are substantial enough to justify another run. The CLI launcher in the [quick start](../README.md#quick-start) is leaner when starting a new task from the terminal.

The MCP server also retains a separate `delegate_task` tool for Relay's optional provider API gateway. That tool requires a Relay key and configured real provider route; it is not used by the native Codex skill. Real provider calls may incur charges. See [gateway operations](operations.md) for that advanced integration.
