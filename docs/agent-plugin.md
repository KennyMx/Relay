# Relay for coding agents

[Back to Relay](../README.md) · [Plugin files](../plugins/relay) · [Gateway setup](../README.md#quick-start)

Relay can offload a small, self-contained text task from Codex or Claude Code to the provider and model you configured in your own gateway. The plugin supplies a real MCP `delegate_task` tool and an opt-in `cheap-task` skill. It does not replace the host agent's model or intercept its tool calls. A lower-cost model can use more tokens, so the measurable goal is lower *estimated spend on delegated tasks*, not fewer total tokens by definition.

## Set up

1. Start Relay with [Docker Compose](../README.md#quick-start). The default mock route works without external API keys. For real answers, configure [real mode](../README.md#use-your-own-provider) with a lower-cost model as the `chat` route's primary target. A fallback may increase cost; leave it unset or choose another appropriately priced model. Use current pricing from your provider. The local mock configuration also offers `fast`, `balanced`, and `reasoning` routes for testing route selection; they return simulated text.
2. In the local operator console, create a Relay API key with a modest request quota. Keep the raw key private. Set `RELAY_KEY` in your shell environment through your preferred secret manager or an interactive prompt, and leave `RELAY_GATEWAY_URL` unset for the local `http://127.0.0.1:8080` gateway. If your gateway is remote, set `RELAY_GATEWAY_URL` to its **HTTPS origin**. The CLI refuses non-loopback HTTP and HTTP redirects so it will not forward the key to a redirected origin. Do not put the key in plugin files, Git, or a prompt.
3. With Go 1.26+ installed, run `go install ./cmd/relay-agent` from the repository root. Ensure `$(go env GOPATH)/bin` is on your `PATH`. Run `relay-agent check`; it reads `/health` and `/v1/routes` without making a completion call. The plugin starts `relay-agent mcp` over standard I/O; no extra server port is needed. Restart your agent after setting `RELAY_KEY` so its plugin process inherits the variable.

Try a task directly:

```sh
relay-agent ask --route chat --max-tokens 256 <<'RELAY_TASK'
Explain the difference between a mutex and a semaphore in two sentences.
RELAY_TASK
```

`--route auto` is available only if you configured `auto_routing` in Relay's JSON file mode. For small, explicit tasks, a fixed cheap route avoids classification overhead; turn on Jev only if its measured benefit exceeds its latency and cost. The simpler real mode has one `chat` route. `relay-agent ask --json` returns the normalized Relay response for scripts. Each `ask` or MCP tool call uses one Relay quota token; the request ID can be inspected in the gateway console or `GET /v1/requests/{id}`. `simulated=true` identifies mock usage, which is not a real provider charge.

## Load the plugin

For **Claude Code**, launch from the repository with:

```sh
claude --plugin-dir ./plugins/relay
```

Invoke `/relay:cheap-task` when you want a bounded task delegated. The skill calls the plugin's `relay.delegate_task` MCP tool and returns the answer with a receipt. Claude Code [documents local plugin testing with `--plugin-dir`](https://code.claude.com/docs/en/plugins#test-your-plugins-locally).

For **Codex**, the repository includes a [local plugin marketplace](../.agents/plugins/marketplace.json). Open this repository as a trusted project, restart the desktop app, then install **Relay** from the repository's plugin source. Ask Codex to use `relay:cheap-task` for a self-contained question. Codex [documents repo-local marketplaces](https://developers.openai.com/plugins/build/plugins#build-your-own-curated-plugin-list). The `relay-agent` executable must be on the host's `PATH` for either plugin host.

Neither plugin sends requests automatically. The skill instructs the host agent to keep edits, broad context, secrets, and verification local, and to treat delegated text as untrusted. Each real request may cost money. Relay records the selected provider, usage, latency, and estimate.

## Inspect modeled cost difference

Use the ledger report to compare Relay's estimate with a user-chosen reference model at **the same observed token counts**. Enter that model's current USD-per-million input/output prices yourself:

```sh
relay-agent report --reference-input 5 --reference-output 10 --limit 100
```

This reads only the authenticated key's most recent 100 ledger entries; it separates real and simulated successful requests, counts input/output tokens, and includes recorded classifier cost in Relay's estimate. It may include non-plugin calls made with that key, so use a dedicated key for plugin-only reporting. `--json` provides structured output. Prices are operator inputs, not scraped values. The comparison is a counterfactual price calculation, not measured host-agent billing, answer-quality equivalence, or guaranteed net savings. Mock results must never be presented as money saved.

The local verification run used the `fast` mock route for one request. Relay recorded **12 simulated input tokens**, **4 simulated output tokens**, and a **$0.00000200** completion estimate. At the illustrative reference rates above, the same 16 tokens price at **$0.00010000**, a **$0.00009800 modeled difference**. Both figures are simulated; this confirms the accounting path, not real savings. To make a real claim, run representative delegated tasks against a real provider, inspect the `REAL` row, and evaluate answer quality and any host-agent subscription charges separately.
