package router

import (
	"context"
	"errors"
	"time"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/provider"
)

var ErrRoute = errors.New("unknown route or provider")

type Target struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
type Route struct {
	Targets []Target `json:"targets"`
}
type Attempt struct {
	Number      int            `json:"number"`
	Provider    string         `json:"provider"`
	Model       string         `json:"model"`
	StartedAt   time.Time      `json:"started_at"`
	LatencyMS   int64          `json:"latency_ms"`
	Status      string         `json:"status"`
	ErrorCode   string         `json:"error_code,omitempty"`
	Usage       provider.Usage `json:"usage"`
	CostNanoUSD int64          `json:"cost_nano_usd"`
}
type Router struct {
	Auto           *AutoPolicy
	Classifier     classifier.Classifier
	ClassifierMode string
	Providers      map[string]provider.Provider
	Routes         map[string]Route
	Default        string
	AttemptTimeout time.Duration
	MaxAttempts    int
}

// Select uses the requested provider as the first target, retaining subsequent
// configured targets as fallbacks. It never forwards an arbitrary model name.
func (r *Router) Select(model, explicit string) ([]Target, error) {
	if model == "" {
		model = r.Default
	}
	route, ok := r.Routes[model]
	if !ok {
		return nil, ErrRoute
	}
	start := 0
	if explicit != "" {
		start = -1
		for i, t := range route.Targets {
			if t.Provider == explicit {
				start = i
				break
			}
		}
		if start < 0 {
			return nil, ErrRoute
		}
	}
	targets := route.Targets[start:]
	if len(targets) == 0 {
		return nil, ErrRoute
	}
	for _, t := range targets {
		if r.Providers[t.Provider] == nil {
			return nil, ErrRoute
		}
	}
	return targets, nil
}
func (r *Router) Complete(ctx context.Context, targets []Target, req provider.Request) (provider.Result, []Attempt, error) {
	attempts := make([]Attempt, 0, len(targets))
	var last error = ErrRoute
	limit := r.MaxAttempts
	if limit < 1 {
		limit = 1
	}
	for i, target := range targets {
		if i >= limit {
			break
		}
		if err := ctx.Err(); err != nil {
			return provider.Result{}, attempts, err
		}
		started := time.Now().UTC()
		attemptCtx, cancel := context.WithTimeout(ctx, r.AttemptTimeout)
		req.Model = target.Model
		result, err := r.Providers[target.Provider].Complete(attemptCtx, req)
		cancel()
		a := Attempt{Number: i + 1, Provider: target.Provider, Model: target.Model, StartedAt: started, LatencyMS: time.Since(started).Milliseconds(), Status: "success", Usage: result.Usage}
		if err != nil {
			a.Status = "error"
			a.ErrorCode = provider.ErrorCode(err)
		}
		attempts = append(attempts, a)
		if err == nil {
			return result, attempts, nil
		}
		last = err
		if ctx.Err() != nil {
			return provider.Result{}, attempts, ctx.Err()
		}
		if !provider.Retryable(err) {
			break
		}
	}
	return provider.Result{}, attempts, last
}
