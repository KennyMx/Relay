# Automatic model routing

Send `"model":"auto"` to select a configured route from request complexity. Explicit route aliases keep working. Each target is a provider/model pair; a provider is not itself a complexity tier.

## Jev setup

1. Keep the default mock completion providers while testing. In the ignored `.env`, set `JEV_API_KEY` to your TypeSafe key and `RELAY_CLASSIFIER=jev`.
2. Run `docker compose up --build -d --wait` to load the environment.
3. Connect to the console and select **auto**. The routing panel identifies whether Jev or the offline baseline is active.

Jev is an external, metered classifier. Enabling it sends the request's messages and output-token limit to [TypeSafe's official API](https://docs.typesafe.ai/api). It may consume credits even when completion providers are mocks. The repository ships with `RELAY_CLASSIFIER=local`, which uses a deterministic Go rule baseline and makes no network calls. No JEV credential is needed to run the tests or the local service.

```sh
curl -sS http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $RELAY_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"auto","messages":[{"role":"user","content":"Design a distributed rate limiter with regional failover."}]}'
```

## Policy

`auto_routing` in [relay.json](../config/relay.json) maps `simple`, `standard`, and `complex` to the `fast`, `balanced`, and `reasoning` routes. Each route has ordered targets and existing bounded provider fallback. Set `default_route` to `auto` if requests that omit `model` should also classify; the shipped API default remains `chat` for compatibility. The console initially selects `auto`.

The Jev adapter asks one Choice question through `POST https://api.typesafe.ai/v1/systemone`. It validates the returned category, probability distribution, confidence, and usage. The configured confidence threshold is 0.65; it is a policy choice, not a proven optimal threshold. Classification does not generate the completion or guarantee that the selected model can answer correctly.

| Condition | Behavior |
| --- | --- |
| Valid, confident classification | Use the configured tier route |
| Confidence below threshold | Use `fallback_route` (`balanced` by default) |
| Timeout, 429, authorization error, unavailable or malformed response | Use fallback route and record the sanitized reason |
| Client cancellation / overall deadline | Stop; do not start a completion provider |
| Explicit route | Bypass classification |
| `auto` plus explicit provider | Bypass classification; start at that provider within `fallback_route` |

There is at most **one classifier call per request**, bounded by a 1,500 ms deadline and the overall request deadline. No classifier retries. Classification runs only after key validation, quota admission, and the pending ledger insert. Classifier failure is different from completion-provider fallback: `fallback_count` counts completion attempts only.

## Recorded decisions and costs

Every finalized request includes `routing.mode`, selected `routing.route`, `routing.reason`, and (for automatic selection) classification source, model, tier, confidence, probabilities, latency, usage, and estimated cost. The same object appears in the completion response, request detail, and PostgreSQL `requests.routing` JSONB column. No prompt, raw key, completion text, or upstream error body is persisted.

`cost_nano_usd` continues to mean **completion** cost. `routing.classification.cost_nano_usd` is separate. The console shows classification cost separately even when completion usage is simulated. Failed classifications have zero *known* cost; the upstream may still bill a timed-out operation.

Jev pricing is configurable in `auto_routing`. The included estimate uses 42 nano-USD per input token and zero per output token, based on [TypeSafe's published input pricing](https://typesafe.ai/) when this integration was built. Confirm your account's current rates before relying on estimates. No billing reconciliation or spending cap is provided.

For real completions, adapt [real.example.json](../config/real.example.json), replace all model placeholders and illustrative prices, remove providers you do not use, and set their keys in `.env`. Changing the completion targets does not require client-side API changes.

## Reproduce the evaluation

The committed [12-case corpus](benchmarks/cases.json) has manually assigned reference tiers. It is a small smoke evaluation, not a representative benchmark or proof of completion quality. The local rules were not trained on it. Jev can disagree with its labels; the reference is subjective.

```sh
# Offline, no API calls
docker compose exec -T gateway relay-evaluate > docs/benchmarks/local.json

# Explicit opt-in: 12 Jev calls, consumes credits; completions remain mock-only
docker compose exec -T gateway relay-evaluate -live > docs/benchmarks/jev.json
```

The evaluator rejects configs containing real completion providers and limits corpora to 50 cases. Results include UTC timestamp, Go/platform metadata, per-case wall time, classification probabilities, selected route, and illustrative completion costs. Timing covers classification and route resolution, including network/TLS for Jev; it excludes the completion, Redis, and PostgreSQL. Requests are sequential, with no warmup or retries; p50/p95 use nearest ranks.

The README charts are generated from these JSON files by `go run ./cmd/charts`. Cost bars compare the same mock token usage at the selected route's illustrative rate against sending every case to the configured reasoning route. They exclude classifier charges and do not measure real-model savings or answer quality.
