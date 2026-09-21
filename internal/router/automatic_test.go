package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/provider"
)

type classifyFunc func(context.Context, provider.Request) (classifier.Result, error)

func (f classifyFunc) Classify(c context.Context, r provider.Request) (classifier.Result, error) {
	return f(c, r)
}
func autoRouter() *Router {
	return &Router{Default: "auto", Auto: &AutoPolicy{Routes: map[string]string{"simple": "fast", "standard": "balanced", "complex": "reasoning"}, FallbackRoute: "balanced", TimeoutMS: 10, MinConfidence: 0.65}, Providers: map[string]provider.Provider{"mock": provider.Mock{}}, Routes: map[string]Route{"fast": {Targets: []Target{{"mock", "small"}}}, "balanced": {Targets: []Target{{"mock", "medium"}}}, "reasoning": {Targets: []Target{{"mock", "large"}}}}, Classifier: classifier.Local{}, MaxAttempts: 2, AttemptTimeout: time.Second}
}
func TestAutoRoutesAndOverrides(t *testing.T) {
	r := autoRouter()
	for prompt, model := range map[string]string{"hi": "small", "Explain Go": "medium", "Prove a theorem": "large"} {
		targets, d, err := r.Resolve(context.Background(), "auto", "", provider.Request{Messages: []provider.Message{{Role: "user", Content: prompt}}})
		if err != nil || targets[0].Model != model || d.Mode != "auto" || d.Reason != "classified" {
			t.Fatal(targets, d, err)
		}
	}
	r.Classifier = classifyFunc(func(context.Context, provider.Request) (classifier.Result, error) {
		t.Fatal("explicit route called classifier")
		return classifier.Result{}, nil
	})
	for _, tc := range []struct{ route, explicit, model string }{{"fast", "", "small"}, {"auto", "mock", "medium"}} {
		targets, d, err := r.Resolve(context.Background(), tc.route, tc.explicit, provider.Request{})
		if err != nil || targets[0].Model != tc.model || d.Classification != nil {
			t.Fatal(d, err)
		}
	}
}
func TestAutoDegradesAndCancels(t *testing.T) {
	low := 0.2
	for _, tc := range []struct {
		name string
		fn   classifyFunc
	}{
		{"low_confidence", func(context.Context, provider.Request) (classifier.Result, error) {
			return classifier.Result{Tier: "simple", Confidence: &low}, nil
		}},
		{"classifier_auth_failed", func(context.Context, provider.Request) (classifier.Result, error) {
			return classifier.Result{}, classifier.ErrAuth
		}},
		{"classifier_timeout", func(ctx context.Context, _ provider.Request) (classifier.Result, error) {
			<-ctx.Done()
			return classifier.Result{}, ctx.Err()
		}},
		{"classifier_invalid_response", func(context.Context, provider.Request) (classifier.Result, error) {
			return classifier.Result{Tier: "arbitrary-route"}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := autoRouter()
			r.Classifier = tc.fn
			targets, d, err := r.Resolve(context.Background(), "auto", "", provider.Request{})
			if err != nil || targets[0].Model != "medium" || d.Reason != tc.name {
				t.Fatal(d, err)
			}
		})
	}
	r := autoRouter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	targets, _, err := r.Resolve(ctx, "auto", "", provider.Request{})
	if !errors.Is(err, context.Canceled) || len(targets) != 0 {
		t.Fatal("ignored client cancellation")
	}
}

func TestAutomaticSelectionKeepsProviderFallback(t *testing.T) {
	r := autoRouter()
	r.Providers["limited"] = provider.Mock{Scenario: "429"}
	r.Routes["fast"] = Route{Targets: []Target{{"limited", "small"}, {"mock", "medium"}}}
	req := provider.Request{Messages: []provider.Message{{Role: "user", Content: "Hello"}}}
	targets, d, err := r.Resolve(context.Background(), "auto", "", req)
	if err != nil {
		t.Fatal(err)
	}
	_, attempts, err := r.Complete(context.Background(), targets, req)
	if err != nil || d.Route != "fast" || len(attempts) != 2 || attempts[0].Status != "error" || attempts[1].Status != "success" {
		t.Fatal(d, attempts, err)
	}
}
