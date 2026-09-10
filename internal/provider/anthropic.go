package provider

import (
	"context"
	"strings"
)

type Anthropic struct{ HTTP }

func (p Anthropic) Complete(ctx context.Context, r Request) (Result, error) {
	var system []string
	messages := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		if m.Role == "system" {
			system = append(system, m.Content)
		} else {
			messages = append(messages, m)
		}
	}
	body := struct {
		Model     string    `json:"model"`
		Messages  []Message `json:"messages"`
		System    string    `json:"system,omitempty"`
		MaxTokens int       `json:"max_tokens"`
	}{r.Model, messages, strings.Join(system, "\n"), r.MaxTokens}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage *struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := p.post(ctx, "/v1/messages", map[string]string{"x-api-key": p.APIKey, "anthropic-version": "2023-06-01"}, body, &out); err != nil {
		return Result{}, err
	}
	if out.Usage == nil {
		return Result{}, &Error{502, "invalid_upstream_response"}
	}
	var content strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}
	return validResult(content.String(), out.Usage.Input, out.Usage.Output)
}
