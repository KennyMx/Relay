# Design decisions

Relay is a single Go service backed by PostgreSQL and Redis. It is deliberately small enough to trace a request from HTTP admission to its persisted provider attempts.

## Request lifecycle

1. Validate the host/origin, bound concurrent work, and establish an overall deadline.
2. Hash the bearer key and look it up in PostgreSQL. Check revocation on every request.
3. Validate the chat payload and resolve a configured route. Arbitrary upstream URLs are never accepted.
4. Execute one atomic Redis token-bucket operation. All fallbacks share this single quota charge.
5. Insert a pending ledger row before calling a provider or classifier.
6. For `auto`, classify with Jev (or the labeled offline baseline); use the configured default route on low confidence or classifier failure.
7. Attempt ordered targets with individual deadlines, at most once per target and at most the configured attempt limit.
8. Finalize the request and attempts in one PostgreSQL transaction, using an independent three-second persistence deadline.
9. Return normalized usage, estimated cost, provider/model, fallback count, and request ID.

## Shared rate limiting

An in-process counter would multiply a key's allowance when more gateways are added. Relay instead uses a Lua script to read, refill, consume, and expire a bucket atomically in Redis. Redis TIME supplies a common clock; an idle bucket expires only after it would have fully refilled. Integration tests issue concurrent requests to check admission bounds.

Redis is an availability dependency: losing it produces 503, not unlimited access. AOF reduces restart loss but is not a guarantee against losing the latest updates. Request quotas are not dollar budgets or provider-token limits.

## Fallback and duplicate work

A 429, timeout, or selected 5xx can advance to the next configured target. Authentication and other non-retryable failures stop. Caller cancellation stops further attempts; there is no unbounded retry loop.

A timeout does not prove the upstream request failed. It may have completed while its response was lost. Fallback can therefore create work and charges at more than one provider. Relay does not claim exactly-once execution or billing. Failed attempts record zero known usage; that means unknown billing, not necessarily free billing. A caller retry after a ledger finalization failure can also duplicate provider work.

## Ledger consistency

Request finalization and its attempt rows commit together. The initial pending row makes a crash during upstream work visible. There is no transaction spanning Redis, PostgreSQL, and remote providers: a quota token can be consumed even if the initial ledger insert fails. It is not refunded because retries must not accidentally mint quota.

A crash before finalization can leave a pending row without attempt detail. Relay does not guess its outcome or reconcile provider invoices. Request history uses offset pagination ordered by timestamp and ID; new arrivals can shift page boundaries. Refresh returns the current page, and sending a completion returns to the newest page.

## Data and access boundaries

Raw Relay keys are generated randomly, shown once, and stored as SHA-256 hashes. The console keeps credentials in memory, refuses fetch redirects, and clears sensitive content on disconnect. Request history is scoped to the current key. Prompts, generated text, provider credentials, and raw upstream errors are not persisted in the ledger.

The admin token provisions and revokes keys; it is not an account system. Read [SECURITY.md](../SECURITY.md) before exposing the service beyond localhost.

## Evidence and limits

Tests use real PostgreSQL and Redis for storage, isolation, revocation, and concurrent quotas; provider wire protocols use local HTTP fixtures. The executable `relay-verify` checks the running service's success, rate-limit/server-error/timeout fallback, ledger entries, quota exhaustion, and revocation. Mock usage is deterministic and explicitly simulated.

There are no claims of measured production throughput, distributed database replication, consensus, or live-provider certification. Those would require different evidence. This project demonstrates service integration and failure handling within the documented boundaries.

## Complexity policy

Jev is optional external decision infrastructure. A single bounded call selects a tier; configuration maps tiers to provider/model targets. This separates classification from execution. The offline rules are an explicit local baseline, not a substitute presented as Jev. See [routing policy and evaluation](automatic-routing.md). Model capability assignments are operator-maintained; a tier is not a guarantee of answer quality.
