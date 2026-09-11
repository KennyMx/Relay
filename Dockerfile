FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay ./cmd/relay && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /relay-verify ./cmd/verify

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 relay
WORKDIR /app
COPY --from=build /relay /usr/local/bin/relay
COPY --from=build /relay-verify /usr/local/bin/relay-verify
COPY config /app/config
USER relay
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=4s --start-period=10s CMD ["relay", "healthcheck"]
ENTRYPOINT ["relay"]
