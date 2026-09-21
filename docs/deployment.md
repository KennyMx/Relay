# Public workspace on Vercel

[Live site](https://relay-three-iota.vercel.app) · [Workspace](https://relay-three-iota.vercel.app/workspace)

Vercel runs `cmd/server`, a small Go HTTP server serving the embedded website and
`POST /v1/try`. It imports the same router, local classifier, mock providers, and
pricing calculator as the full gateway. No JavaScript backend, database service,
or provider credentials are needed for this deployment.

## Two runtime modes

| | Public workspace | Self-hosted gateway |
| --- | --- | --- |
| Entry point | `cmd/server` | `cmd/relay` |
| Interface | `/workspace`, `/v1/try` | `/console`, `/v1/chat/completions` |
| Classification | Local rules | Local rules or opt-in Jev |
| Completions | Deterministic simulation | Mock or configured real providers |
| Access | No signup | Hashed Relay keys |
| Quotas | 8 in-flight requests per instance | Redis per-key token buckets |
| History | Latest 20 requests in browser memory | PostgreSQL request and attempt ledger |
| Credentials | None | Private environment variables |

The public workspace shows actual routing decisions, ordered provider attempts,
and measured server latency. Token usage and costs are **simulated**. It does not
pretend to exercise Redis or PostgreSQL; use Docker and `relay-verify` to verify
those guarantees. `/v1/keys`, `/v1/requests`, and `/v1/chat/completions` are not
registered in the public server. The public server does not read external API keys.

Public requests are limited to 4,000 message characters, a 16 KiB JSON body,
two provider attempts, and a one-second execution deadline. There is no distributed
public traffic quota. Hosting remains subject to your Vercel plan limits; provider
API calls cannot consume credits in this mode. Relay does not persist public
messages, and does not log request bodies. Hosting access logs are managed by Vercel.

## Deploy your own

With Go 1.26 locally, `go run ./cmd/server` runs the same public server on port 8080.
For Vercel, install/use its CLI and authenticate to your own account:

```sh
npx vercel login
npx vercel deploy --prod
```

`vercel.json` chooses the Go runtime and builds `cmd/server`. Do not add provider,
admin, Redis, or database credentials to the public project's environment.
`.vercelignore` excludes local credential files and review artifacts from uploads.
The Go server honors the platform's `PORT` variable.

The current project was deployed through the CLI. GitHub automatic deployments
are not connected; subsequent releases use the same deploy command. Vercel's Git
integration can be enabled separately by the repository owner.

For the full persistent gateway, follow the [Docker quick start](../README.md#quick-start).
PostgreSQL and Redis run alongside it; they are not bundled into the Vercel site.
