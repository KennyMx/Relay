package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddedConsole(t *testing.T) {
	handler := Handler()
	for _, test := range []struct {
		path        string
		contentType string
		contains    string
	}{
		{"/", "text/html", "The right route."},
		{"/console", "text/html", "Relay Console"},
		{"/workspace", "text/html", "Request workspace"},
		{"/architecture", "text/html", "Inside the gateway."},
		{"/assets/favicon.svg", "image/svg+xml", "<svg"},
		{"/assets/workspace.js", "text/javascript", "/v1/try"},
		{"/assets/styles.css", "text/css", "--accent"},
		{"/assets/app.js", "text/javascript", "/v1/chat/completions"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", response.Code)
			}
			if !strings.Contains(response.Header().Get("Content-Type"), test.contentType) {
				t.Fatalf("unexpected content type: %s", response.Header().Get("Content-Type"))
			}
			body, _ := io.ReadAll(response.Body)
			if !strings.Contains(string(body), test.contains) {
				t.Fatalf("missing %q", test.contains)
			}
		})
	}
}

func TestConsoleDoesNotCatchUnknownPaths(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", response.Code)
	}
}
