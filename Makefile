.PHONY: test check run integration verify

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
