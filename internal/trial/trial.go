// Package trial exposes a bounded public request workspace. It uses Relay's
// production router and local provider implementations, never external API keys.
package trial

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/keys"
	"github.com/KennyMx/Relay/internal/pricing"
	"github.com/KennyMx/Relay/internal/provider"
	"github.com/KennyMx/Relay/internal/router"
	"github.com/KennyMx/Relay/internal/webui"
)

func newRouter() *router.Router {
	routes := map[string]router.Route{}
	for _, name := range []string{"fast", "balanced", "reasoning"} {
		routes[name] = router.Route{Targets: []router.Target{{Provider: "mock", Model: "mock-" + name}}}
	}
	return &router.Router{
		Providers: map[string]provider.Provider{"mock": provider.Mock{}, "mock-limited": provider.Mock{Scenario: "429"}, "mock-failed": provider.Mock{Scenario: "500"}, "mock-timeout": provider.Mock{Scenario: "timeout"}},
		Routes:    routes, Default: "auto", MaxAttempts: 2, AttemptTimeout: 120 * time.Millisecond, Classifier: classifier.Local{}, ClassifierMode: "local",
		Auto: &router.AutoPolicy{Routes: map[string]string{"simple": "fast", "standard": "balanced", "complex": "reasoning"}, FallbackRoute: "balanced", TimeoutMS: 50, MinConfidence: .65},
	}
}

// Handler has a fixed mock-only provider allowlist and never reads credentials.
// The in-flight bound is per instance, not a distributed per-key quota.
func Handler() http.Handler {
	routing := newRouter()
	rates := pricing.Table{}
	for i, name := range []string{"fast", "balanced", "reasoning"} {
		rate := []int64{100, 1000, 5000}[i]
		rates["mock/mock-"+name] = pricing.Rate{Input: rate, Output: 2 * rate}
	}
	inflight := make(chan struct{}, 8)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
		}
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			fail(405, "method_not_allowed")
			return
		}
		if !sameOrigin(r) {
			fail(403, "untrusted_origin")
			return
		}
		select {
		case inflight <- struct{}{}:
			defer func() { <-inflight }()
		default:
			w.Header().Set("Retry-After", "1")
			fail(429, "workspace_busy")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var in struct {
			Message  string `json:"message"`
			Route    string `json:"route"`
			Scenario string `json:"scenario"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil || decoder.Decode(new(any)) != io.EOF {
			fail(400, "invalid_json")
			return
		}
		in.Message = strings.TrimSpace(in.Message)
		if in.Message == "" || !utf8.ValidString(in.Message) || utf8.RuneCountInString(in.Message) > 4000 {
			fail(400, "message_must_be_1_to_4000_characters")
			return
		}
		if in.Route == "" {
			in.Route = "auto"
		}
		if in.Scenario == "" {
			in.Scenario = "success"
		}
		if routing.ValidateSelection(in.Route, "") != nil {
			fail(400, "unknown_route")
			return
		}
		upstream := ""
		switch in.Scenario {
		case "success":
		case "rate_limit":
			upstream = "mock-limited"
		case "server_error", "unavailable":
			upstream = "mock-failed"
		case "timeout":
			upstream = "mock-timeout"
		default:
			fail(400, "unknown_scenario")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		started := time.Now()
		req := provider.Request{Messages: []provider.Message{{Role: "user", Content: in.Message}}, MaxTokens: 256}
		targets, decision, err := routing.Resolve(ctx, in.Route, "", req)
		if err != nil {
			fail(408, "request_canceled")
			return
		}
		if upstream != "" {
			failed := router.Target{Provider: upstream, Model: targets[0].Model}
			if in.Scenario == "unavailable" {
				targets = []router.Target{failed}
			} else {
				targets = append([]router.Target{failed}, targets...)
			}
		}
		result, attempts, callErr := routing.Complete(ctx, targets, req)
		var cost int64
		for i := range attempts {
			if attempts[i].Status == "success" {
				attempts[i].CostNanoUSD, _ = rates.Estimate(attempts[i].Provider, attempts[i].Model, attempts[i].Usage)
				cost += attempts[i].CostNanoUSD
			}
		}
		status := "success"
		errorCode := ""
		if callErr != nil {
			status = "error"
			errorCode = provider.ErrorCode(callErr)
		}
		// Even failed public requests clearly identify simulated accounting.
		result.Usage.Simulated = true
		code := http.StatusOK
		if callErr != nil {
			code = http.StatusBadGateway
		}
		id := keys.ID()
		w.Header().Set("X-Request-ID", id)
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "created_at": time.Now().UTC(), "status": status, "error_code": errorCode, "routing": decision, "attempts": attempts, "usage": result.Usage, "cost_nano_usd": cost, "latency_ms": float64(time.Since(started).Microseconds()) / 1000, "fallback_count": max(0, len(attempts)-1), "content": result.Content, "mode": "public", "persisted": false})
	})
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host) && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
	}
	return true
}

func PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/try", Handler())
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","mode":"public","dependencies":"not_required"}`)
	})
	mux.HandleFunc("GET /v1/runtime", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"mode":"public","operator_console":false}`)
	})
	mux.Handle("/", webui.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		mux.ServeHTTP(w, r)
	})
}
