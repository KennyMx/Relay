// Package provider defines Relay's non-streaming, text-only provider contract.
package provider

import (
	"context"
	"errors"
	"net"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Request struct {
	Model     string    `json:"model"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	Simulated    bool  `json:"simulated"`
}
type Result struct {
	Content string `json:"content"`
	Usage   Usage  `json:"usage"`
}
type Provider interface {
	Complete(context.Context, Request) (Result, error)
}

// Error contains only sanitized metadata; never persist upstream response bodies.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string { return e.Code }
func Retryable(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Status == 429 || pe.Status == 500 || pe.Status == 502 || pe.Status == 503 || pe.Status == 504
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
func ErrorCode(err error) string {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "provider_error"
}
