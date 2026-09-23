# Security

Relay's full gateway is self-hosted for a trusted operator and the people to whom that operator issues keys. The public site has a separate, stateless workspace with simulated completions; it has no provider credentials, PostgreSQL, or Redis access. No security review or scanner can guarantee the absence of vulnerabilities.

## Defaults and controls

- `relay init` creates independent 256-bit random admin/database secrets in an exclusive, mode-0600 `.env` file. Never publish this file, terminal output containing credentials, or database dumps. Startup refuses missing/known example admin credentials. PostgreSQL requires a configured password.
- Relay keys use 256 random bits. Only SHA-256 hashes are stored. Key administration requires the separate admin bearer token; request history is scoped to the calling Relay key. Provider credentials stay on the server.
- Browser credentials live only in memory. Reload/disconnect requires reconnection. Disconnect clears credential fields, response content, and pending key values, and invalidates in-flight authenticated responses. The browser refuses API redirects.
- The gateway rejects unapproved Host headers and cross-origin browser calls, including cross-site Fetch Metadata. This protects the loopback console against DNS rebinding. These checks do not replace bearer authentication.
- Prompt/response text is not persisted in the ledger. Upstream error bodies are not returned or logged. Provider URLs are fixed, HTTPS-only, and do not follow redirects. SQL uses bound parameters.
- Request bodies, provider responses, timeouts, fallback attempts, active gateway work (64 requests), and per-key quotas are bounded. Quotas count requests, not spend.
- Compose exposes only the gateway on loopback. Its container runs without root, a shell, Linux capabilities, or a writable root filesystem. PostgreSQL/Redis use persistent volumes on the internal Compose network.

## Before exposing an instance

Use TLS, an exact `RELAY_ALLOWED_HOSTS` list, and network access controls. Keep the admin token with trusted operators, and prefer restricting `/v1/keys` at your reverse proxy. Add edge connection/rate limits for unauthenticated traffic; Relay's per-key quotas are not full DDoS protection. Do not publish PostgreSQL or Redis ports. Redis trusts the internal Docker network: use authentication/TLS and network isolation if it runs elsewhere. Protect your Docker daemon and volume backups as privileged access.

The database connection used by Compose owns its schema so it can apply migrations. For a hardened external deployment, separate migration credentials from a restricted runtime role. Configure real-provider billing limits separately: a timed-out provider may bill work for which Relay never receives usage. Rotate credentials whenever access changes or exposure is suspected.

## Upgrading from earlier local credentials

Earlier commits contained deliberately public local passwords. They are not private secrets, but they must not be used for a running service. Current startup rejects the old admin examples. Updating code does not revoke existing Relay keys or automatically rotate credentials in existing data volumes.

1. Back up your private `.env` and data using your normal protected backup process. Generate new credentials into a separate file with `relay init /path/to/new-private.env`; it never overwrites files. Preserve your configured provider keys when updating the active `.env`.
2. Rotate the existing PostgreSQL role through its local administrative connection using interactive `\password relay` in `psql`; set it to the new `POSTGRES_PASSWORD`. This avoids putting the password in shell history or SQL command text. Do not delete the data volume to rotate credentials.
3. Recreate the gateway and PostgreSQL containers with `docker compose up --build -d --wait` to load the new environment. Verify `/health` and issue a new Relay key.
4. Revoke any Relay keys potentially created while a known admin credential was in use. Use `DELETE /v1/keys/{id}` with the new admin token, or have the database administrator revoke affected rows if you do not know their IDs.

## Checks

`make security` runs Go's vulnerability and static-analysis scanners, UI credential lifecycle regression tests, and a redacted Gitleaks scan of all Git refs. CI repeats these checks, plus race-enabled PostgreSQL/Redis integration tests and full-stack verification. Actions are pinned to commit hashes; Dependabot checks Go, Docker, and action dependencies weekly.

The September 2026 review found and replaced vulnerable dependency versions flagged as GO-2026-5004 (pgx) and GO-2026-5970 (x/text). It also removed shared default credentials and fixed hidden UI credential/data retention. Scanner results are a point-in-time check, not a certification; update and rerun regularly.

## Reporting

Please use GitHub's private vulnerability reporting option if available on this repository. Otherwise contact the maintainer through their GitHub profile and ask for a private channel before sharing exploit details or credentials. Do not post live secrets in public issues.
