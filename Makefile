.PHONY: test check run integration

test:
	go test -race ./...
check:
	go vet ./...
run:
	go run ./cmd/relay
integration:
	RELAY_INTEGRATION=1 go test -race ./internal/ratelimit ./internal/store ./internal/api
