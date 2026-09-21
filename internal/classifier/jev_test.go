package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KennyMx/Relay/internal/provider"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const valid = `{"model":"jev-fixture","answers":{"complexity":{"type":"choice","choice":"complex","confidence":0.9,"probabilities":{"simple":0.02,"standard":0.03,"complex":0.95}}},"usage":{"input_tokens":200,"output_tokens":20}}`

func TestJevContract(t *testing.T) {
	j := Jev{APIKey: "test-credential", Model: "jev-latest", InputNanoUSDPerToken: 42, Client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != Endpoint || r.Header.Get("Authorization") != "Bearer test-credential" || r.Method != "POST" {
			t.Fatal("wrong request")
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["state"] == nil || !strings.Contains(string(body["questions"]), `"complexity"`) {
			t.Fatal("wrong contract")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(valid)), Header: make(http.Header)}, nil
	})}}
	got, err := j.Classify(context.Background(), provider.Request{Messages: []provider.Message{{Role: "user", Content: "Design a distributed database"}}})
	if err != nil || got.Tier != "complex" || got.Usage.TotalTokens != 220 || got.CostNanoUSD != 8400 || got.Usage.Simulated {
		t.Fatal(got, err)
	}
}
func TestJevRejectsInvalidResponses(t *testing.T) {
	for _, body := range []string{`{}`, `not json`, strings.Replace(valid, `"complex","confidence"`, `"injected","confidence"`, 1), strings.Replace(valid, `"confidence":0.9`, `"confidence":2`, 1), strings.Replace(valid, `"input_tokens":200`, `"input_tokens":-1`, 1), strings.Replace(valid, `"complex":0.95`, `"complex":0.1`, 1), strings.Repeat("x", 65537)} {
		j := Jev{Client: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		if _, err := j.Classify(context.Background(), provider.Request{}); !errors.Is(err, ErrResponse) {
			t.Fatalf("accepted malformed result: %v", err)
		}
	}
}
func TestJevErrorsAndCancellation(t *testing.T) {
	for _, status := range []int{401, 429, 500, 302} {
		j := Jev{Client: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("secret prompt")), Header: make(http.Header)}, nil
		})}}
		_, err := j.Classify(context.Background(), provider.Request{})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	j := Jev{Client: &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}}
	if _, err := j.Classify(ctx, provider.Request{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestLocal(t *testing.T) {
	for prompt, tier := range map[string]string{"Hello": "simple", "Explain a binary tree": "standard", "Design a distributed database": "complex"} {
		got, err := (Local{}).Classify(context.Background(), provider.Request{Messages: []provider.Message{{Role: "user", Content: prompt}}})
		if err != nil || got.Tier != tier || got.Source != "local" || !got.Usage.Simulated || got.Confidence != nil {
			t.Fatal(got, err)
		}
	}
}
