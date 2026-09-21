package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/config"
	"github.com/KennyMx/Relay/internal/provider"
	"github.com/KennyMx/Relay/internal/ratelimit"
	"github.com/KennyMx/Relay/internal/store"
)

type gateStore struct {
	Ledger
	beginErr error
	final    store.Record
	began    bool
}

func (s *gateStore) ValidateKey(context.Context, string) (store.Key, error) {
	return store.Key{ID: "test", RequestsPerMinute: 1, Burst: 1}, nil
}
func (s *gateStore) Begin(context.Context, store.Record) error      { s.began = true; return s.beginErr }
func (s *gateStore) Finish(_ context.Context, r store.Record) error { s.final = r; return nil }

type gateLimiter struct{ allowed bool }

func (l gateLimiter) Allow(context.Context, string, int, int) (ratelimit.Decision, error) {
	return ratelimit.Decision{Allowed: l.allowed}, nil
}

type spyClassifier struct {
	calls int
	store *gateStore
	wait  bool
}

func (s *spyClassifier) Classify(ctx context.Context, _ provider.Request) (classifier.Result, error) {
	s.calls++
	if !s.store.began {
		panic("classification before ledger")
	}
	if s.wait {
		<-ctx.Done()
		return classifier.Result{Source: "jev"}, ctx.Err()
	}
	return classifier.Result{Tier: "simple", Source: "local"}, nil
}
func TestClassificationAdmissionAndPersistence(t *testing.T) {
	t.Setenv("RELAY_CLASSIFIER", "local")
	for _, tc := range []struct {
		name          string
		allowed       bool
		beginErr      error
		cancel        bool
		status, calls int
	}{
		{"quota rejected", false, nil, false, 429, 0},
		{"ledger unavailable", true, errors.New("offline"), false, 503, 0},
		{"success", true, nil, false, 200, 1},
		{"cancellation", true, nil, true, 504, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.Load("../../config/relay.json")
			if err != nil {
				t.Fatal(err)
			}
			r, err := cfg.BuildRouter()
			if err != nil {
				t.Fatal(err)
			}
			db := &gateStore{beginErr: tc.beginErr}
			spy := &spyClassifier{store: db, wait: tc.cancel}
			r.Classifier = spy
			s := Server{AllowedHosts: []string{"example.com"}, Store: db, Limiter: gateLimiter{tc.allowed}, Router: r, Pricing: cfg.Pricing, Timeout: time.Second}
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Authorization", "Bearer rl_live_"+strings.Repeat("a", 64))
			if tc.cancel {
				ctx, cancel := context.WithTimeout(req.Context(), 10*time.Millisecond)
				defer cancel()
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			if w.Code != tc.status || spy.calls != tc.calls {
				t.Fatal(w.Code, spy.calls, w.Body.String())
			}
			if tc.cancel && (len(db.final.Attempts) != 0 || db.final.Status != "error" || db.final.Routing == nil) {
				t.Fatal("cancellation not persisted", db.final)
			}
		})
	}
}
