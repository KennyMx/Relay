// Package api exposes Relay's authenticated HTTP API.
package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KennyMx/Relay/internal/keys"
	"github.com/KennyMx/Relay/internal/pricing"
	"github.com/KennyMx/Relay/internal/provider"
	"github.com/KennyMx/Relay/internal/ratelimit"
	"github.com/KennyMx/Relay/internal/router"
	"github.com/KennyMx/Relay/internal/store"
	"github.com/KennyMx/Relay/internal/webui"
	"github.com/jackc/pgx/v5"
)

type Ledger interface {
	CreateKey(context.Context, string, int, int) (store.Key, string, error)
	ValidateKey(context.Context, string) (store.Key, error)
	RevokeKey(context.Context, string) (bool, error)
	Begin(context.Context, store.Record) error
	Finish(context.Context, store.Record) error
	List(context.Context, string, int, int) ([]store.Record, error)
	Get(context.Context, string, string) (store.Record, error)
}
type Limiter interface {
	Allow(context.Context, string, int, int) (ratelimit.Decision, error)
}
type Server struct {
	Store      Ledger
	Limiter    Limiter
	Router     *router.Router
	Pricing    pricing.Table
	AdminToken string
	Timeout    time.Duration
	Health     func(context.Context) error
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /v1/keys", s.admin(s.createKey))
	mux.HandleFunc("DELETE /v1/keys/{id}", s.admin(s.revokeKey))
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.chat))
	mux.HandleFunc("GET /v1/routes", s.auth(s.routes))
	mux.HandleFunc("GET /v1/requests", s.auth(s.list))
	mux.HandleFunc("GET /v1/requests/{id}", s.auth(s.get))
	mux.Handle("/", webui.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		ctx, cancel := context.WithTimeout(r.Context(), s.Timeout)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

type routeInfo struct {
	Name    string          `json:"name"`
	Targets []router.Target `json:"targets"`
}

func (s *Server) routes(w http.ResponseWriter, _ *http.Request, key store.Key) {
	routes := make([]routeInfo, 0, len(s.Router.Routes))
	for name, route := range s.Router.Routes {
		targets := append([]router.Target(nil), route.Targets...)
		routes = append(routes, routeInfo{Name: name, Targets: targets})
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Name == s.Router.Default {
			return true
		}
		if routes[j].Name == s.Router.Default {
			return false
		}
		return routes[i].Name < routes[j].Name
	})
	write(w, http.StatusOK, map[string]any{"default_route": s.Router.Default, "routes": routes, "key": key})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": strings.ReplaceAll(code, "_", " ")}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid_json")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "invalid_json")
		return false
	}
	return true
}
func bearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}
func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, b := sha256.Sum256([]byte(bearer(r))), sha256.Sum256([]byte(s.AdminToken))
		if s.AdminToken == "" || subtle.ConstantTimeCompare(a[:], b[:]) != 1 {
			fail(w, 401, "invalid_admin_token")
			return
		}
		next(w, r)
	}
}
func (s *Server) auth(next func(http.ResponseWriter, *http.Request, store.Key)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := bearer(r)
		if _, err := keys.Hash(raw); err != nil {
			fail(w, 401, "invalid_api_key")
			return
		}
		key, err := s.Store.ValidateKey(r.Context(), raw)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 401, "invalid_api_key")
			return
		}
		if err != nil {
			fail(w, 503, "key_store_unavailable")
			return
		}
		next(w, r, key)
	}
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.Health != nil && s.Health(ctx) != nil {
		fail(w, 503, "dependencies_unavailable")
		return
	}
	write(w, 200, map[string]string{"status": "ok"})
}
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		RPM   int    `json:"requests_per_minute"`
		Burst int    `json:"burst"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 || in.RPM < 1 || in.RPM > 100000 || in.Burst < 1 || in.Burst > 100000 {
		fail(w, 400, "invalid_key_settings")
		return
	}
	k, raw, err := s.Store.CreateKey(r.Context(), in.Name, in.RPM, in.Burst)
	if err != nil {
		fail(w, 503, "key_store_unavailable")
		return
	}
	write(w, 201, struct {
		store.Key
		APIKey string `json:"api_key"`
	}{k, raw})
}
func (s *Server) revokeKey(w http.ResponseWriter, r *http.Request) {
	ok, err := s.Store.RevokeKey(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, 503, "key_store_unavailable")
		return
	}
	if !ok {
		fail(w, 404, "key_not_found")
		return
	}
	w.WriteHeader(204)
}

type chatRequest struct {
	Model     string             `json:"model"`
	Provider  string             `json:"provider,omitempty"`
	Messages  []provider.Message `json:"messages"`
	MaxTokens int                `json:"max_tokens,omitempty"`
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request, key store.Key) {
	var in chatRequest
	if !decode(w, r, &in) {
		return
	}
	if in.MaxTokens == 0 {
		in.MaxTokens = 256
	}
	if len(in.Messages) == 0 || len(in.Messages) > 100 || in.MaxTokens < 1 || in.MaxTokens > 8192 {
		fail(w, 400, "invalid_chat_request")
		return
	}
	hasUser := false
	seenConversation := false
	for _, m := range in.Messages {
		if strings.TrimSpace(m.Content) == "" || (m.Role != "system" && m.Role != "user" && m.Role != "assistant") || (m.Role == "system" && seenConversation) {
			fail(w, 400, "invalid_messages")
			return
		}
		if m.Role != "system" {
			seenConversation = true
		}
		if m.Role == "user" {
			hasUser = true
		}
	}
	if !hasUser {
		fail(w, 400, "user_message_required")
		return
	}
	targets, err := s.Router.Select(in.Model, in.Provider)
	if err != nil {
		fail(w, 400, "unknown_route_or_provider")
		return
	}
	decision, err := s.Limiter.Allow(r.Context(), key.ID, key.RequestsPerMinute, key.Burst)
	if err != nil {
		fail(w, 503, "rate_limiter_unavailable")
		return
	}
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(decision.Remaining, 10))
	if !decision.Allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(decision.RetryAfter.Seconds())))))
		fail(w, 429, "quota_exceeded")
		return
	}
	started := time.Now().UTC()
	route := in.Model
	if route == "" {
		route = s.Router.Default
	}
	record := store.Record{ID: keys.ID(), KeyID: key.ID, CreatedAt: started, Route: route, Status: "pending"}
	w.Header().Set("X-Request-ID", record.ID)
	if err = s.Store.Begin(r.Context(), record); err != nil {
		fail(w, 503, "ledger_unavailable")
		return
	}
	result, attempts, callErr := s.Router.Complete(r.Context(), targets, provider.Request{Messages: in.Messages, MaxTokens: in.MaxTokens})
	record.Attempts = attempts
	record.Status = "success"
	record.LatencyMS = time.Since(started).Milliseconds()
	record.FallbackCount = max(0, len(attempts)-1)
	if len(attempts) > 0 {
		last := attempts[len(attempts)-1]
		record.Model = last.Model
		record.Provider = last.Provider
	}
	for i := range record.Attempts {
		a := &record.Attempts[i]
		if a.Status == "success" {
			a.CostNanoUSD, err = s.Pricing.Estimate(a.Provider, a.Model, a.Usage)
			if err != nil {
				callErr = err
				break
			}
			record.CostNanoUSD += a.CostNanoUSD
			record.Usage = a.Usage
		}
	}
	if callErr != nil {
		record.Status = "error"
		record.ErrorCode = provider.ErrorCode(callErr)
	}
	// Client disconnects and upstream timeouts must not cancel ledger persistence.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer cancel()
	if err = s.Store.Finish(persistCtx, record); err != nil {
		slog.Error("ledger finalization failed", "request_id", record.ID)
		fail(w, 503, "ledger_finalization_failed")
		return
	}
	if callErr != nil {
		status := 502
		if errors.Is(callErr, context.DeadlineExceeded) {
			status = 504
		}
		fail(w, status, record.ErrorCode)
		return
	}
	write(w, 200, map[string]any{"id": record.ID, "object": "chat.completion", "created": record.CreatedAt.Unix(), "model": record.Model, "provider": record.Provider, "route": record.Route, "choices": []any{map[string]any{"index": 0, "message": provider.Message{Role: "assistant", Content: result.Content}}}, "usage": record.Usage, "cost_nano_usd": record.CostNanoUSD, "latency_ms": record.LatencyMS, "fallback_count": record.FallbackCount})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request, key store.Key) {
	limit, offset := 20, 0
	for name, dest := range map[string]*int{"limit": &limit, "offset": &offset} {
		if value := r.URL.Query().Get(name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				fail(w, 400, "invalid_pagination")
				return
			}
			*dest = n
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		fail(w, 400, "invalid_pagination")
		return
	}
	records, err := s.Store.List(r.Context(), key.ID, limit, offset)
	if err != nil {
		fail(w, 503, "ledger_unavailable")
		return
	}
	write(w, 200, map[string]any{"data": records, "limit": limit, "offset": offset})
}
func (s *Server) get(w http.ResponseWriter, r *http.Request, key store.Key) {
	record, err := s.Store.Get(r.Context(), key.ID, r.PathValue("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "request_not_found")
		return
	}
	if err != nil {
		fail(w, 503, "ledger_unavailable")
		return
	}
	write(w, 200, record)
}
