package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdapters(t *testing.T) {
	for _, name := range []string{"openai", "anthropic", "cohere"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "test-model" {
					t.Error(body)
				}
				switch name {
				case "openai":
					if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" || body["max_completion_tokens"] != float64(50) {
						t.Error("invalid OpenAI request")
					}
					_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`))
				case "anthropic":
					if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "test-secret" || r.Header.Get("anthropic-version") == "" || body["system"] != "be brief" || len(body["messages"].([]any)) != 1 {
						t.Error("invalid Anthropic request")
					}
					_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":10,"output_tokens":2}}`))
				case "cohere":
					if r.URL.Path != "/v2/chat" || r.Header.Get("Authorization") != "Bearer test-secret" || body["max_tokens"] != float64(50) {
						t.Error("invalid Cohere request")
					}
					_, _ = w.Write([]byte(`{"message":{"content":[{"type":"text","text":"hello"}]},"usage":{"billed_units":{"input_tokens":10,"output_tokens":2},"tokens":{"input_tokens":20,"output_tokens":5}}}`))
				}
			}))
			defer server.Close()
			h := HTTP{Client: server.Client(), BaseURL: server.URL, APIKey: "test-secret"}
			adapters := map[string]Provider{"openai": OpenAI{h}, "anthropic": Anthropic{h}, "cohere": Cohere{h}}
			result, err := adapters[name].Complete(context.Background(), Request{Model: "test-model", Messages: []Message{{"system", "be brief"}, {"user", "hi"}}, MaxTokens: 50})
			if err != nil || result.Content != "hello" || result.Usage.TotalTokens != 12 || result.Usage.Simulated {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}
func TestHTTPFailures(t *testing.T) {
	for _, status := range []int{400, 401, 429, 500, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("private prompt and secret"))
			}))
			defer s.Close()
			_, err := (OpenAI{HTTP{BaseURL: s.URL}}).Complete(context.Background(), Request{})
			if err == nil || (Retryable(err) != (status == 429 || status >= 500)) {
				t.Fatal(err)
			}
			if ErrorCode(err) != err.Error() {
				t.Fatal("unsanitized error")
			}
		})
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"choices":[]}`)) }))
	defer s.Close()
	_, err := (OpenAI{HTTP{BaseURL: s.URL}}).Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("accepted missing content and usage")
	}
}
