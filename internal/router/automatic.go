package router

import (
	"context"
	"errors"
	"time"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/provider"
)

type AutoPolicy struct {
	Routes                map[string]string `json:"routes"`
	FallbackRoute         string            `json:"fallback_route"`
	TimeoutMS             int               `json:"timeout_ms"`
	MinConfidence         float64           `json:"min_confidence"`
	Model                 string            `json:"model"`
	InputNanoUSDPerToken  int64             `json:"input_nano_usd_per_token"`
	OutputNanoUSDPerToken int64             `json:"output_nano_usd_per_token"`
}

type Decision struct {
	Mode           string             `json:"mode"`
	Route          string             `json:"route"`
	Reason         string             `json:"reason"`
	Classification *classifier.Result `json:"classification,omitempty"`
}

// ValidateSelection does no network work and runs before quota admission.
func (r *Router) ValidateSelection(model, explicit string) error {
	if model == "" {
		model = r.Default
	}
	if model == "auto" && r.Auto != nil {
		if explicit != "" {
			_, err := r.Select(r.Auto.FallbackRoute, explicit)
			return err
		}
		return nil
	}
	_, err := r.Select(model, explicit)
	return err
}

// Resolve runs after authentication, quotas and the initial ledger record.
func (r *Router) Resolve(ctx context.Context, model, explicit string, req provider.Request) ([]Target, Decision, error) {
	if model == "" {
		model = r.Default
	}
	d := Decision{Mode: "explicit", Route: model, Reason: "requested_route"}
	if model != "auto" || r.Auto == nil {
		targets, err := r.Select(model, explicit)
		return targets, d, err
	}
	d.Route = r.Auto.FallbackRoute
	if explicit != "" {
		d.Reason = "provider_override"
		targets, err := r.Select(d.Route, explicit)
		return targets, d, err
	}
	d.Mode = "auto"
	classifyCtx, cancel := context.WithTimeout(ctx, time.Duration(r.Auto.TimeoutMS)*time.Millisecond)
	started := time.Now()
	result, err := r.Classifier.Classify(classifyCtx, req)
	cancel()
	result.LatencyMS = time.Since(started).Milliseconds()
	d.Classification = &result
	if ctx.Err() != nil {
		d.Reason = "request_canceled"
		return nil, d, ctx.Err()
	}
	switch {
	case err != nil:
		d.Reason = "classifier_unavailable"
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			d.Reason = "classifier_timeout"
		case errors.Is(err, classifier.ErrAuth):
			d.Reason = "classifier_auth_failed"
		case errors.Is(err, classifier.ErrLimited):
			d.Reason = "classifier_rate_limited"
		case errors.Is(err, classifier.ErrResponse):
			d.Reason = "classifier_invalid_response"
		}
	case !classifier.ValidTier(result.Tier):
		d.Reason = "classifier_invalid_response"
	case result.Confidence != nil && *result.Confidence < r.Auto.MinConfidence:
		d.Reason = "low_confidence"
	default:
		d.Route = r.Auto.Routes[result.Tier]
		d.Reason = "classified"
	}
	targets, selectErr := r.Select(d.Route, "")
	return targets, d, selectErr
}
