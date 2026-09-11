package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestBrowserBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, host, origin, site string
		allowed                  bool
	}{
		{"CLI", "localhost:8080", "", "", true},
		{"console", "localhost:8080", "http://localhost:8080", "same-origin", true},
		{"TLS proxy", "localhost:8080", "https://localhost:8080", "same-origin", true},
		{"IPv6", "[::1]:8080", "http://[::1]:8080", "same-origin", true},
		{"DNS rebinding", "attacker.example:8080", "http://attacker.example:8080", "same-origin", false},
		{"forged forwarding", "attacker.example", "", "", false},
		{"cross origin", "localhost:8080", "https://attacker.example", "", false},
		{"different port", "localhost:8080", "http://localhost:9000", "same-site", false},
		{"opaque origin", "localhost:8080", "null", "", false},
		{"metadata", "localhost:8080", "", "cross-site", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/health", nil)
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("X-Forwarded-Host", "localhost")
			w := httptest.NewRecorder()
			(&Server{Timeout: time.Second}).Handler().ServeHTTP(w, r)
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("got %d, want %d", w.Code, want)
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, "https://relay.example/health", nil)
	w := httptest.NewRecorder()
	(&Server{Timeout: time.Second, AllowedHosts: []string{"relay.example"}}).Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatal("explicit host configuration rejected")
	}
}

func TestConcurrentWorkIsBounded(t *testing.T) {
	entered := make(chan struct{}, 64)
	release := make(chan struct{})
	h := (&Server{Timeout: time.Minute, Health: func(ctx context.Context) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}).Handler()
	var wg sync.WaitGroup
	defer wg.Wait()
	defer close(release)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://localhost/health", nil))
		}()
	}
	for i := 0; i < 64; i++ {
		<-entered
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost/health", nil))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Fatal("unbounded work accepted")
	}
}
