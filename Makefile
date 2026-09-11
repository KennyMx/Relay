.PHONY: test check run integration demo

test:
	go test -race ./...
check:
	go vet ./...
	@test -z "$$(gofmt -l cmd internal migrations)"
run:
	go run ./cmd/relay
integration:
	docker compose -f docker-compose.yml -f docker-compose.test.yml run --rm test
demo:
	docker compose exec -T gateway relay-demo
