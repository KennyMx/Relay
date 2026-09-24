# Relay for coding agents

[Back to Relay](../README.md) · [Plugin files](../plugins/relay) · [Gateway setup](../README.md#quick-start)

Relay can offload a small, self-contained text task from Codex or Claude Code to the provider and model you configured in your own gateway. This is **opt-in delegation**, not a replacement for the host agent's model. The plugin cannot reroute the host's internal tool calls or change its active model per turn. Because a lower-cost model can use more tokens, the practical goal is lower *estimated spend on delegated tasks*, not fewer total tokens by definition.

## Set up

1. Start Relay with [Docker Compose](../README.md#quick-start). The default mock route works without external API keys. For real answers, configure [real mode](../README.md#use-your-own-provider) with a lower-cost model as the `chat` route's primary target. A fallback may increase cost; leave it unset or choose another appropriately priced model. Use current pricing from your provider.
2. In the local operator console, create a Relay API key with a modest request quota. Keep the raw key private. Set `RELAY_KEY` in your shell environment through your preferred secret manager or an interactive prompt, and leave `RELAY_GATEWAY_URL` unset for the local `http://127.0.0.1:8080` gateway. If your gateway is remote, set `RELAY_GATEWAY_URL` to its **HTTPS origin**. The CLI refuses non-loopback HTTP and HTTP redirects so it will not forward the key to a redirected origin. Do not put the key in plugin files, Git, or a prompt.
3. With Go 1.26+ installed, run `go install ./cmd/relay-agent` from the repository root. Ensure `$(go env GOPATH)/bin` is on your `PATH`. Run `relay-agent check`; it reads `/health` and `/v1/routes` without making a completion call.

Try a task directly:

```sh
relay-agent ask --route chat --max-tokens 256 <<'RELAY_TASK'
Explain the difference between a mutex and a semaphore in two sentences.
RELAY_TASK
```

`--route auto` is available only if you configured `auto_routing` in Relay's JSON file mode. The simpler real mode has one `chat` route. `relay-agent ask --json` returns the normalized Relay response for scripts. Each `ask` call uses one Relay quota token; the printed request ID can be inspected in the gateway console or `GET /v1/requests/{id}`. `simulated=true` identifies mock usage, which is not a real provider charge.

## Load the plugin

For **Claude Code**, launch from the repository with:

```sh
claude --plugin-dir ./plugins/relay
```

Invoke `/relay:cheap-task` when you want a bounded task delegated. Claude Code [documents local plugin testing with `--plugin-dir`](https://code.claude.com/docs/en/plugins#test-your-plugins-locally).

For **Codex**, the repository includes a [local plugin marketplace](../.agents/plugins/marketplace.json). Open this repository as a trusted project, restart the desktop app, then install **Relay** from the repository's plugin source. Ask Codex to use `relay:cheap-task` for a self-contained question. Codex [documents repo-local marketplaces](https://developers.openai.com/plugins/build/plugins#build-your-own-curated-plugin-list). The CLI executable still needs to be on the host's `PATH`.

Neither plugin sends requests automatically. The skill instructs the host agent to keep edits, broad context, secrets, and verification local, and to treat delegated text as untrusted. Each real request may cost money. Relay records the selected provider, usage, latency, and estimate, but it does not know the host agent's subscription billing or compute a verified net saving across a whole session.
