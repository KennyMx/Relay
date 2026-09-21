.PHONY: test check run integration verify security evaluate charts

test:
	go test -race ./...
check:
	go vet ./...
	@test -z "$$(gofmt -l cmd internal migrations)"
run:
	go run ./cmd/relay
integration:
	docker compose -f docker-compose.yml -f docker-compose.test.yml run --rm test
verify:
	docker compose exec -T gateway relay-verify
evaluate:
	docker compose exec -T gateway relay-evaluate
charts:
	go run ./cmd/charts
security:
	go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
	go run github.com/securego/gosec/v2/cmd/gosec@v2.29.0 ./...
	node --test internal/webui/*test.cjs
	docker run --rm -v "$(CURDIR):/src:ro" ghcr.io/gitleaks/gitleaks@sha256:c00b6bd0aeb3071cbcb79009cb16a60dd9e0a7c60e2be9ab65d25e6bc8abbb7f git /src --config /src/.gitleaks.toml --redact --log-opts=--all
