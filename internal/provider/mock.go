package provider

import (
	"context"
	"strings"
)

// Mock's behavior is selected by server configuration, never by prompt content.
type Mock struct{ Scenario string }

func (m Mock) Complete(ctx context.Context, r Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	switch m.Scenario {
	case "429":
		return Result{}, &Error{429, "upstream_rate_limited"}
	case "500":
		return Result{}, &Error{500, "upstream_unavailable"}
	case "timeout":
		<-ctx.Done()
		return Result{}, ctx.Err()
	case "success", "":
	default:
		return Result{}, &Error{400, "invalid_mock_scenario"}
	}
	var words int64
	for _, msg := range r.Messages {
		words += int64(len(strings.Fields(msg.Content)))
	}
	content := "Hello from Relay mock."
	if r.MaxTokens > 0 && r.MaxTokens < 4 {
		content = strings.Join(strings.Fields(content)[:r.MaxTokens], " ")
	}
	output := int64(len(strings.Fields(content)))
	return Result{Content: content, Usage: Usage{InputTokens: words, OutputTokens: output, TotalTokens: words + output, Simulated: true}}, nil
}
