package store

import (
	"context"
	"errors"
	"github.com/KennyMx/Relay/internal/keys"
	"github.com/KennyMx/Relay/internal/provider"
	"github.com/KennyMx/Relay/internal/router"
	"github.com/KennyMx/Relay/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
	"time"
)

func TestPostgresLedgerAndKeys(t *testing.T) {
	if os.Getenv("RELAY_INTEGRATION") != "1" {
		t.Skip("set RELAY_INTEGRATION=1 with DATABASE_URL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = migrations.Apply(ctx, pool); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	s := Store{pool}
	k, raw, err := s.CreateKey(ctx, "integration", 60, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM relay_keys WHERE id=$1", k.ID)
	got, err := s.ValidateKey(ctx, raw)
	if err != nil || got.ID != k.ID {
		t.Fatal(got, err)
	}
	var stored string
	if err = pool.QueryRow(ctx, "SELECT key_hash FROM relay_keys WHERE id=$1", k.ID).Scan(&stored); err != nil || stored == raw || len(stored) != 64 {
		t.Fatal("raw key stored", err)
	}
	r := Record{ID: keys.ID(), KeyID: k.ID, CreatedAt: time.Now().UTC(), Route: "chat"}
	if err = s.Begin(ctx, r); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM requests WHERE id=$1", r.ID)
	defer pool.Exec(ctx, "DELETE FROM provider_attempts WHERE request_id=$1", r.ID)
	r.Routing = &router.Decision{Mode: "auto", Route: "balanced", Reason: "classified"}
	r.Route = "balanced"
	r.Model = "mock-v1"
	r.Provider = "mock"
	r.Status = "success"
	r.FallbackCount = 1
	r.LatencyMS = 5
	r.CostNanoUSD = 10000
	r.Usage = provider.Usage{InputTokens: 2, OutputTokens: 4, TotalTokens: 6, Simulated: true}
	r.Attempts = []router.Attempt{{Number: 1, Provider: "mock-429", Model: "mock-v1", StartedAt: r.CreatedAt, Status: "error", ErrorCode: "upstream_rate_limited"}, {Number: 2, Provider: r.Provider, Model: r.Model, StartedAt: r.CreatedAt, Status: "success", Usage: r.Usage, CostNanoUSD: r.CostNanoUSD}}
	if err = s.Finish(ctx, r); err != nil {
		t.Fatal(err)
	}
	read, err := s.Get(ctx, k.ID, r.ID)
	if err != nil || len(read.Attempts) != 2 || read.Usage != r.Usage || read.CostNanoUSD != 10000 || read.FallbackCount != 1 || read.Route != "balanced" || read.Routing == nil || read.Routing.Mode != "auto" {
		t.Fatal(read, err)
	}
	if _, err = s.Get(ctx, "another-key", r.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("cross-key read", err)
	}
	list, err := s.List(ctx, k.ID, 10, 0)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	// A duplicate attempt forces rollback, including the preceding request update.
	r.CostNanoUSD = 999
	if err = s.Finish(ctx, r); err == nil {
		t.Fatal("expected duplicate failure")
	}
	read, _ = s.Get(ctx, k.ID, r.ID)
	if read.CostNanoUSD != 10000 {
		t.Fatal("partial transaction committed")
	}
	ok, err := s.RevokeKey(ctx, k.ID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, err = s.ValidateKey(ctx, raw); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("revoked key accepted", err)
	}
}
