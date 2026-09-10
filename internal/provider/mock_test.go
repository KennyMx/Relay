package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMock(t *testing.T) {
	r := Request{Messages: []Message{{Role: "user", Content: "hello world"}}, MaxTokens: 20}
	a, err := (Mock{}).Complete(context.Background(), r)
	b, _ := (Mock{}).Complete(context.Background(), r)
	if err != nil || a != b || !a.Usage.Simulated || a.Usage.TotalTokens != 6 {
		t.Fatalf("unexpected: %+v %v", a, err)
	}
	for _, scenario := range []string{"429", "500", "timeout"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		_, err := (Mock{scenario}).Complete(ctx, r)
		cancel()
		if !Retryable(err) {
			t.Errorf("%s: %v", scenario, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (Mock{}).Complete(ctx, r)
	if !errors.Is(err, context.Canceled) || Retryable(err) {
		t.Fatal(err)
	}
}
