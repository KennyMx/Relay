package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeDoer func(*http.Request) (*http.Response, error)

func (f fakeDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }

func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestAskCallsConfiguredRouteWithoutLeakingKey(t *testing.T) {
	t.Setenv("RELAY_GATEWAY_URL", "http://127.0.0.1:8080")
	t.Setenv("RELAY_KEY", "fixture-key")
	var output bytes.Buffer
	called := false
	err := run([]string{"ask", "--route", "cheap", "--max-tokens", "64"}, strings.NewReader("summarize this sentence"), &output, fakeDoer(func(r *http.Request) (*http.Response, error) {
		called = true
		if r.URL.String() != "http://127.0.0.1:8080/v1/chat/completions" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatal("incorrect gateway request")
		}
		data, _ := io.ReadAll(r.Body)
		if !bytes.Contains(data, []byte(`"model":"cheap"`)) || !bytes.Contains(data, []byte(`"max_tokens":64`)) || !bytes.Contains(data, []byte("summarize this sentence")) {
			t.Fatalf("incorrect request body: %s", data)
		}
		return reply(200, `{"id":"req-1","model":"cheap-v1","provider":"openai","route":"cheap","choices":[{"message":{"content":"Short answer."}}],"usage":{"input_tokens":4,"output_tokens":3,"simulated":false},"cost_nano_usd":12000}`), nil
	}))
	if err != nil || !called || !strings.Contains(output.String(), "Short answer.") || !strings.Contains(output.String(), "estimated=$0.00001200") {
		t.Fatalf("unexpected result: %q, %v", output.String(), err)
	}
	if strings.Contains(output.String(), "fixture-key") {
		t.Fatal("key leaked into output")
	}
}

func TestAskRejectsOversizedOrMissingInputBeforeNetwork(t *testing.T) {
	t.Setenv("RELAY_KEY", "fixture-key")
	t.Setenv("RELAY_GATEWAY_URL", "http://localhost:8080")
	client := fakeDoer(func(*http.Request) (*http.Response, error) {
		t.Fatal("unexpected network request")
		return nil, nil
	})
	for _, input := range []string{" ", strings.Repeat("x", maxPromptBytes+1)} {
		if err := run([]string{"ask"}, strings.NewReader(input), io.Discard, client); err == nil {
			t.Fatal("expected input rejection")
		}
	}
	if err := run([]string{"ask", "--max-tokens", "513"}, strings.NewReader("hello"), io.Discard, client); err == nil {
		t.Fatal("expected output-token cap")
	}
}

func TestGatewayFailuresStaySanitized(t *testing.T) {
	t.Setenv("RELAY_KEY", "fixture-key")
	t.Setenv("RELAY_GATEWAY_URL", "http://localhost:8080")
	err := run([]string{"ask"}, strings.NewReader("hello"), io.Discard, fakeDoer(func(*http.Request) (*http.Response, error) {
		return reply(429, `{"error":{"code":"quota_exceeded","message":"private upstream detail"}}`), nil
	}))
	if err == nil || !strings.Contains(err.Error(), "quota_exceeded") || strings.Contains(err.Error(), "private upstream detail") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckAndGatewayURL(t *testing.T) {
	t.Setenv("RELAY_KEY", "fixture-key")
	t.Setenv("RELAY_GATEWAY_URL", "http://localhost:8080")
	var output bytes.Buffer
	err := run([]string{"check"}, strings.NewReader(""), &output, fakeDoer(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/health" {
			if r.Header.Get("Authorization") != "" {
				t.Fatal("health should not receive the key")
			}
			return reply(200, `{"status":"ok"}`), nil
		}
		if r.URL.Path != "/v1/routes" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatal("unexpected routes request")
		}
		return reply(200, `{"default_route":"chat","routes":[{"name":"chat"}]}`), nil
	}))
	if err != nil || !strings.Contains(output.String(), "default route: chat") {
		t.Fatalf("unexpected check result: %q, %v", output.String(), err)
	}
	for _, raw := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/path", "file:///tmp"} {
		if _, err := gatewayURL(raw); err == nil {
			t.Fatalf("accepted unsafe origin %q", raw)
		}
	}
	if _, err := gatewayURL("https://relay.example.com"); err != nil {
		t.Fatal(err)
	}
}
