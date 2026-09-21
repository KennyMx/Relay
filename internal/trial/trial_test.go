package trial

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicWorkspaceScenarios(t *testing.T) {
	// Public traffic must remain local even in a process with private credentials.
	t.Setenv("RELAY_CLASSIFIER", "jev")
	t.Setenv("JEV_API_KEY", "must-not-be-used")
	h := PublicHandler()
	for _, tc := range []struct {
		scenario         string
		status, attempts int
	}{{"success", 200, 1}, {"rate_limit", 200, 2}, {"server_error", 200, 2}, {"timeout", 200, 2}, {"unavailable", 502, 1}} {
		t.Run(tc.scenario, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/v1/try", strings.NewReader(`{"message":"Design a distributed system","route":"auto","scenario":"`+tc.scenario+`"}`))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			var out struct {
				Routing struct {
					Route          string
					Classification struct{ Source string }
				}
				Attempts  []json.RawMessage
				Usage     struct{ Simulated bool }
				Persisted bool
			}
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Routing.Route != "reasoning" || out.Routing.Classification.Source != "local" || len(out.Attempts) != tc.attempts || !out.Usage.Simulated || out.Persisted {
				t.Fatal(w.Body.String())
			}
		})
	}
}
func TestPublicInputBoundaries(t *testing.T) {
	h := PublicHandler()
	for _, body := range []string{`{}`, `{"message":"hi","route":"unconfigured"}`, `{"message":"hi","scenario":"external"}`, `{"message":"hi","api_key":"private"}`, `{"message":"hi"} {}`, `{"message":"` + strings.Repeat("x", 4001) + `"}`, strings.Repeat("x", 20000)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/try", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/v1/try", strings.NewReader(`{"message":"hi"}`))
	r.Header.Set("Origin", "https://other.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/v1/keys", "/v1/requests", "/v1/chat/completions"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code < 400 {
			t.Fatal("private API exposed", path)
		}
	}
}
func TestPublicPages(t *testing.T) {
	h := PublicHandler()
	for _, path := range []string{"/", "/workspace", "/architecture", "/console", "/health", "/v1/runtime", "/assets/site.css", "/assets/workspace.js"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
}
