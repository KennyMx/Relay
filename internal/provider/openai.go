package provider

import "context"

type OpenAI struct{ HTTP }

func (p OpenAI) Complete(ctx context.Context, r Request) (Result, error) {
	body := struct {
		Model     string    `json:"model"`
		Messages  []Message `json:"messages"`
		MaxTokens int       `json:"max_completion_tokens"`
	}{r.Model, r.Messages, r.MaxTokens}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage *struct {
			Input  int64 `json:"prompt_tokens"`
			Output int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := p.post(ctx, "/v1/chat/completions", map[string]string{"Authorization": "Bearer " + p.APIKey}, body, &out); err != nil {
		return Result{}, err
	}
	if len(out.Choices) == 0 || out.Usage == nil {
		return Result{}, &Error{502, "invalid_upstream_response"}
	}
	return validResult(out.Choices[0].Message.Content, out.Usage.Input, out.Usage.Output)
}
