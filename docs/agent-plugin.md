# Relay for Codex

[Back to Relay](../README.md) · [Plugin files](../plugins/relay)

The Relay Codex plugin exposes `route_codex_task`: an MCP tool that launches one bounded, read-only native Codex CLI subtask on a selected model. It uses your existing Codex login and returns the answer plus Codex-reported input, cached-input, and output tokens. It does not change the parent task's active model.

## Install

On macOS or Linux, install the executable from the repository root:

```sh
go install ./cmd/relay-agent
export PATH="$(go env GOPATH)/bin:$PATH"
```

The plugin launcher checks `RELAY_AGENT_BIN`, then `PATH`, then Go’s default `~/go/bin/relay-agent`. For a custom `GOBIN` or `GOPATH`, set `RELAY_AGENT_BIN` to the absolute executable path in the environment used to launch Codex. No shell profile change is needed for a default Go install.

The repository has a [local plugin marketplace](../.agents/plugins/marketplace.json). Open it as a trusted Codex project, install Relay, and restart Codex so it loads `plugins/relay/.mcp.json`. Ask Codex to use `relay:cheap-task` for a bounded read-only question. The skill calls `relay.route_codex_task`. Use `cheap`, `strong`, or `auto`; the default is `auto`. The `list_codex_models` tool reads the current model catalog and supported reasoning levels. Pass `model` and `effort` to `route_codex_task` for an explicit choice, such as `gpt-5.6-terra` and `xhigh`. Unsupported choices fail before execution. The child run writes only metadata to `.relay/runs.jsonl` in the current workspace. Open `relay-agent serve` locally to inspect it.

The child process inherits Codex authentication, runs with Codex's read-only sandbox, and is marked to prevent another Relay child from recursively routing again. Do not put secrets or full conversation history in the task argument. A nested agent has its own startup/context overhead, so use the plugin for tasks that are substantial enough to justify another run. The CLI launcher in the [quick start](../README.md#quick-start) is leaner when starting a new task from the terminal.

The MCP server also retains a separate `delegate_task` tool for Relay's optional provider API gateway. That tool requires a Relay key and configured real provider route; it is not used by the native Codex skill. Real provider calls may incur charges. See [gateway operations](operations.md) for that advanced integration.

## Model dropdown

Installing Relay adds a skill and MCP tools. The documented Codex plugin interface does not expose a hook for adding a model-picker entry or replacing the inference backend of every chat message. Relay therefore does not appear as a model in that dropdown. Model-provider configuration is a separate integration and requires a compatible inference endpoint; the native launcher is not that endpoint.

For the supported in-chat workflow, ask: “Use Relay to inspect the rate limiter with GPT-5.6 Terra at extra-high reasoning, then show its usage receipt.” Start a new Codex task after installing or updating the plugin so the new tool schema is loaded. The parent task still uses its selected model; Relay launches a separate bounded child.

Sources: [Codex model discovery](https://learn.chatgpt.com/docs/app-server#list-models-modellist), [reasoning configuration](https://learn.chatgpt.com/docs/config-file/config-reference), [plugin capabilities](https://developers.openai.com/plugins/concepts/plugins).
