FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay ./cmd/relay && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay-verify ./cmd/verify

FROM scratch
WORKDIR /app
COPY --from=build /relay /usr/local/bin/relay
COPY --from=build /relay-verify /usr/local/bin/relay-verify
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY config /app/config
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s CMD ["/usr/local/bin/relay", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/relay"]
