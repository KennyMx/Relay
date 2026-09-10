// Package pricing calculates estimates in integer nano-USD (1 USD = 1e9).
package pricing

import (
	"errors"
	"github.com/KennyMx/Relay/internal/provider"
	"math"
)

// Rates are nano-USD per token, numerically equal to milli-USD per million tokens.
// Example: $0.15 / million input tokens = 150 nano-USD / input token.
type Rate struct {
	Input  int64 `json:"input_nano_usd_per_token"`
	Output int64 `json:"output_nano_usd_per_token"`
}
type Table map[string]Rate

func (t Table) Estimate(p, m string, u provider.Usage) (int64, error) {
	rate, ok := t[p+"/"+m]
	if !ok {
		return 0, errors.New("model pricing not configured")
	}
	if rate.Input < 0 || rate.Output < 0 || u.InputTokens < 0 || u.OutputTokens < 0 {
		return 0, errors.New("negative rate or usage")
	}
	if (rate.Input > 0 && u.InputTokens > math.MaxInt64/rate.Input) || (rate.Output > 0 && u.OutputTokens > math.MaxInt64/rate.Output) {
		return 0, errors.New("cost overflow")
	}
	input, output := u.InputTokens*rate.Input, u.OutputTokens*rate.Output
	if input > math.MaxInt64-output {
		return 0, errors.New("cost overflow")
	}
	return input + output, nil
}
