package api

import (
	"context"
	"encoding/json"
	"github.com/KennyMx/Relay/internal/config"
	"github.com/KennyMx/Relay/internal/ratelimit"
	"github.com/KennyMx/Relay/internal/store"
	"github.com/KennyMx/Relay/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAPIIntegration(t *testing.T) {
	if os.Getenv("RELAY_INTEGRATION") != "1" {
		t.Skip("requires PostgreSQL and Redis; see README")
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
	opts, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	rc := redis.NewClient(opts)
	defer rc.Close()
	cfg, err := config.Load("../../config/relay.json")
	if err != nil {
		t.Fatal(err)
	}
	routing, err := cfg.BuildRouter()
	if err != nil {
		t.Fatal(err)
	}
	db := &store.Store{Pool: pool}
	s := Server{AllowedHosts: []string{"example.com"}, Store: db, Limiter: &ratelimit.Bucket{Client: rc}, Router: routing, Pricing: cfg.Pricing, AdminToken: "integration-admin-token-32-characters", Timeout: 3 * time.Second}
	handler := s.Handler()
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	want := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("want %d, got %d: %s", status, w.Code, w.Body.String())
		}
	}
	want(request("POST", "/v1/keys", "", `{"name":"x","requests_per_minute":1,"burst":1}`), 401)
	created := request("POST", "/v1/keys", s.AdminToken, `{"name":"api-integration","requests_per_minute":60,"burst":20}`)
	want(created, 201)
	var key struct {
		ID  string `json:"id"`
		Raw string `json:"api_key"`
	}
	if err = json.Unmarshal(created.Body.Bytes(), &key); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM provider_attempts WHERE request_id IN (SELECT id FROM requests WHERE key_id=$1)", key.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM requests WHERE key_id=$1", key.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM relay_keys WHERE id=$1", key.ID)
		rc.Del(ctx, "relay:bucket:"+key.ID)
	}()
	want(request("POST", "/v1/chat/completions", "bad", `{}`), 401)
	routes := request("GET", "/v1/routes", key.Raw, "")
	want(routes, 200)
	if !strings.Contains(routes.Body.String(), `"default_route":"chat"`) || !strings.Contains(routes.Body.String(), `"targets"`) {
		t.Fatal("route discovery response incomplete", routes.Body.String())
	}
	want(request("POST", "/v1/chat/completions", key.Raw, `{"messages":[],"stream":true}`), 400)
	want(request("POST", "/v1/chat/completions", key.Raw, `{"model":"missing","messages":[{"role":"user","content":"hi"}]}`), 400)
	want(request("POST", "/v1/chat/completions", key.Raw, `{"messages":[{"role":"user","content":"hi"}]} {}`), 400)
	var recordID string
	for _, route := range []string{"chat", "fallback-rate-limit", "fallback-server-error", "fallback-timeout"} {
		response := request("POST", "/v1/chat/completions", key.Raw, `{"model":"`+route+`","messages":[{"role":"user","content":"hello world"}]}`)
		want(response, 200)
		var out struct {
			ID       string `json:"id"`
			Cost     int64  `json:"cost_nano_usd"`
			Fallback int    `json:"fallback_count"`
			Usage    struct {
				Simulated bool `json:"simulated"`
				Total     int  `json:"total_tokens"`
			} `json:"usage"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		expectedFallback := 1
		if route == "chat" {
			expectedFallback = 0
		}
		if out.Cost != 10000 || out.Usage.Total != 6 || !out.Usage.Simulated || out.Fallback != expectedFallback {
			t.Fatalf("%s: %+v", route, out)
		}
		detail := request("GET", "/v1/requests/"+out.ID, key.Raw, "")
		want(detail, 200)
		var record store.Record
		if err = json.Unmarshal(detail.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if len(record.Attempts) != expectedFallback+1 || record.Status != "success" {
			t.Fatal(record)
		}
		recordID = out.ID
	}
	// A caller deadline stops fallback but still persists the attempted provider.
	deadlineCtx, deadlineCancel := context.WithTimeout(ctx, 100*time.Millisecond)
	deadlineReq := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fallback-timeout","messages":[{"role":"user","content":"cancel me"}]}`)).WithContext(deadlineCtx)
	deadlineReq.Header.Set("Authorization", "Bearer "+key.Raw)
	deadlineResponse := httptest.NewRecorder()
	handler.ServeHTTP(deadlineResponse, deadlineReq)
	deadlineCancel()
	want(deadlineResponse, 504)
	canceledRecord, err := db.Get(ctx, key.ID, deadlineResponse.Header().Get("X-Request-ID"))
	if err != nil || canceledRecord.Status != "error" || len(canceledRecord.Attempts) != 1 || canceledRecord.Attempts[0].ErrorCode != "timeout" {
		t.Fatal("canceled request not logged", canceledRecord, err)
	}
	failed := request("POST", "/v1/chat/completions", key.Raw, `{"model":"unavailable","messages":[{"role":"user","content":"hi"}]}`)
	want(failed, 502)
	want(request("GET", "/v1/requests/"+failed.Header().Get("X-Request-ID"), key.Raw, ""), 200)
	explicit := request("POST", "/v1/chat/completions", key.Raw, `{"model":"fallback-rate-limit","provider":"mock","messages":[{"role":"user","content":"hi"}]}`)
	want(explicit, 200)
	if !strings.Contains(explicit.Body.String(), `"fallback_count":0`) {
		t.Fatal("explicit selection did not skip primary")
	}
	want(request("GET", "/v1/requests?limit=2&offset=0", key.Raw, ""), 200)
	want(request("GET", "/v1/requests?limit=0", key.Raw, ""), 400)
	other, raw, err := db.CreateKey(ctx, "isolated", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM provider_attempts WHERE request_id IN (SELECT id FROM requests WHERE key_id=$1)", other.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM requests WHERE key_id=$1", other.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM relay_keys WHERE id=$1", other.ID)
		rc.Del(ctx, "relay:bucket:"+other.ID)
	}()
	want(request("GET", "/v1/requests/"+recordID, raw, ""), 404)
	want(request("POST", "/v1/chat/completions", raw, `{"messages":[{"role":"user","content":"hi"}]}`), 200)
	limited := request("POST", "/v1/chat/completions", raw, `{"messages":[{"role":"user","content":"hi"}]}`)
	want(limited, 429)
	if limited.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry-after")
	}
	want(request("DELETE", "/v1/keys/"+key.ID, key.Raw, ""), 401)
	want(request("DELETE", "/v1/keys/"+key.ID, s.AdminToken, ""), 204)
	want(request("GET", "/v1/requests", key.Raw, ""), 401)
}

func TestHTTPValidation(t *testing.T) {
	s := Server{AllowedHosts: []string{"example.com"}, Timeout: time.Second, AdminToken: "test-admin"}
	h := s.Handler()
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"GET", "/health", "", 200}, {"POST", "/v1/keys", "{}", 401}, {"POST", "/v1/chat/completions", "{}", 401}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatal(w.Code)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing content security policy")
		}
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Relay Console") {
		t.Fatal("operator console unavailable")
	}
	for _, body := range []string{`{} {}`, `{"unknown":true}`, strings.Repeat("x", 70000)} {
		w := httptest.NewRecorder()
		if decode(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &chatRequest{}) {
			t.Fatal("accepted invalid body")
		}
	}
}
