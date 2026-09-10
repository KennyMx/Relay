package provider

import (
	"context"
	"strings"
)

type Cohere struct{ HTTP }

func (p Cohere) Complete(ctx context.Context, r Request) (Result, error) {
	var out struct {
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
		Usage *struct {
			Billed *struct {
				Input  int64 `json:"input_tokens"`
				Output int64 `json:"output_tokens"`
			} `json:"billed_units"`
		} `json:"usage"`
	}
	if err := p.post(ctx, "/v2/chat", map[string]string{"Authorization": "Bearer " + p.APIKey}, r, &out); err != nil {
		return Result{}, err
	}
	if out.Usage == nil || out.Usage.Billed == nil {
		return Result{}, &Error{502, "invalid_upstream_response"}
	}
	var content strings.Builder
	for _, block := range out.Message.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}
	// Cohere distinguishes billed units from raw tokenization; price billed units.
	return validResult(content.String(), out.Usage.Billed.Input, out.Usage.Billed.Output)
}
