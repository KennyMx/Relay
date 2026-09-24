# Public product site

The [public site](https://relay-three-iota.vercel.app) explains Relay and shows a screenshot of actual Codex CLI runs. It does not execute agent tasks, expose provider credentials, or offer a simulated completion playground. `cmd/server` serves embedded product pages through `webui.PublicHandler`; `/workspace`, `/console`, and `/v1/try` are not public routes.

The developer tool runs on a user's own machine:

```sh
go install ./cmd/relay-agent
printf 'Summarize this repository.' | relay-agent run --host codex --mode auto
relay-agent serve
```

`relay-agent serve` binds to `127.0.0.1:8787` by default and reads only that machine's metadata-only `.relay/runs.jsonl` file. The public site has no access to this file or to the user's Codex authentication. It displays no live usage from other users.

The optional PostgreSQL/Redis API gateway is a separate self-hosted process. See [gateway operations](operations.md). Do not deploy its credentialed operator console as part of the public product site.
