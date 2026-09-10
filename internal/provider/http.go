package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// HTTP is shared transport only. Each adapter owns its wire format.
type HTTP struct {
	Client  *http.Client
	BaseURL string
	APIKey  string
}

func (h HTTP) post(ctx context.Context, path string, headers map[string]string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := h.Client
	if client == nil {
		client = &http.Client{}
	}
	// Never forward provider credentials to a redirect target.
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := safe.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{resp.StatusCode, fmt.Sprintf("upstream_http_%d", resp.StatusCode)}
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil {
		return err
	}
	if len(data) > 2<<20 || json.Unmarshal(data, out) != nil {
		return &Error{502, "invalid_upstream_response"}
	}
	return nil
}
func validResult(content string, input, output int64) (Result, error) {
	if content == "" || input < 0 || output < 0 || input > 1_000_000_000 || output > 1_000_000_000 {
		return Result{}, &Error{502, "invalid_upstream_response"}
	}
	return Result{Content: content, Usage: Usage{InputTokens: input, OutputTokens: output, TotalTokens: input + output}}, nil
}
