package router

import (
	"context"
	"github.com/KennyMx/Relay/internal/provider"
	"testing"
	"time"
)

func fixture() *Router {
	return &Router{Providers: map[string]provider.Provider{"primary": provider.Mock{Scenario: "429"}, "secondary": provider.Mock{}, "slow": provider.Mock{Scenario: "timeout"}}, Routes: map[string]Route{"chat": {Targets: []Target{{"primary", "mock-v1"}, {"secondary", "mock-v1"}}}}, Default: "chat", AttemptTimeout: time.Millisecond, MaxAttempts: 3}
}
func TestRoutingAndFallback(t *testing.T) {
	r := fixture()
	targets, err := r.Select("", "")
	if err != nil || targets[0].Provider != "primary" {
		t.Fatal(targets, err)
	}
	result, attempts, err := r.Complete(context.Background(), targets, provider.Request{})
	if err != nil || len(attempts) != 2 || attempts[0].Status != "error" || attempts[1].Status != "success" || !result.Usage.Simulated {
		t.Fatal(attempts, err)
	}
	targets, err = r.Select("chat", "secondary")
	if err != nil || len(targets) != 1 {
		t.Fatal(targets, err)
	}
	for _, v := range [][2]string{{"missing", ""}, {"chat", "missing"}} {
		if _, err = r.Select(v[0], v[1]); err == nil {
			t.Fatal("accepted unknown target")
		}
	}
	r.MaxAttempts = 1
	_, a, err := r.Complete(context.Background(), []Target{{"primary", "m"}, {"secondary", "m"}}, provider.Request{})
	if err == nil || len(a) != 1 {
		t.Fatal(a, err)
	}
}
func TestTimeoutAndCancellation(t *testing.T) {
	r := fixture()
	targets := []Target{{"slow", "m"}, {"secondary", "m"}}
	_, a, err := r.Complete(context.Background(), targets, provider.Request{})
	if err != nil || len(a) != 2 || a[0].ErrorCode != "timeout" {
		t.Fatal(a, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Microsecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	_, a, err = r.Complete(ctx, targets, provider.Request{})
	if err == nil || len(a) != 0 {
		t.Fatal(a, err)
	}
	r.Providers["primary"] = provider.Mock{Scenario: "invalid"}
	_, a, err = r.Complete(context.Background(), []Target{{"primary", "m"}, {"secondary", "m"}}, provider.Request{})
	if err == nil || len(a) != 1 {
		t.Fatal(a, err)
	}
}
