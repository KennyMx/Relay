// Package classifier assigns request complexity without generating completions.
package classifier

import (
	"context"
	"strings"

	"github.com/KennyMx/Relay/internal/provider"
)

type Result struct {
	Tier          string             `json:"tier,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Source        string             `json:"source"`
	Model         string             `json:"model,omitempty"`
	LatencyMS     int64              `json:"latency_ms"`
	Usage         provider.Usage     `json:"usage"`
	CostNanoUSD   int64              `json:"cost_nano_usd"`
}

type Classifier interface {
	Classify(context.Context, provider.Request) (Result, error)
}

func ValidTier(s string) bool { return s == "simple" || s == "standard" || s == "complex" }

// Local is an offline rule baseline, not Jev and not a learned classifier.
// It reports no confidence because its rules have not been calibrated.
type Local struct{}

func (Local) Classify(ctx context.Context, req provider.Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	text := strings.ToLower(b.String())
	tier := "simple"
	if len(text) > 800 || len(req.Messages) > 4 || req.MaxTokens > 1024 {
		tier = "standard"
	}
	for _, s := range []string{"write a function", "compare", "explain", "debug", "code", "```"} {
		if strings.Contains(text, s) {
			tier = "standard"
		}
	}
	for _, s := range []string{"distributed", "prove", "deadlock", "race condition", "architecture", "trade-off", "tradeoff"} {
		if strings.Contains(text, s) {
			tier = "complex"
		}
	}
	if len(text) > 8000 {
		tier = "complex"
	}
	return Result{Tier: tier, Source: "local", Usage: provider.Usage{Simulated: true}}, nil
}
