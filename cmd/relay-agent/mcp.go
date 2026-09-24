package main

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type delegateInput struct {
	Task      string `json:"task" jsonschema:"A small, self-contained text task. Do not include secrets or private repository content."`
	Route     string `json:"route,omitempty" jsonschema:"Configured Relay route; defaults to chat."`
	MaxTokens int    `json:"max_tokens,omitempty" jsonschema:"Maximum output tokens, from 1 to 512; defaults to 256."`
}

type delegateOutput struct {
	Answer               string `json:"answer"`
	RequestID            string `json:"request_id"`
	Provider             string `json:"provider"`
	Model                string `json:"model"`
	Route                string `json:"route"`
	InputTokens          int    `json:"input_tokens"`
	OutputTokens         int    `json:"output_tokens"`
	EstimatedCostNanoUSD int64  `json:"estimated_cost_nano_usd"`
	Simulated            bool   `json:"simulated"`
}

func newMCPServer(client httpDoer) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "relay", Version: "0.2.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delegate_task",
		Description: "Opt-in delegation of one bounded text task to your configured Relay route. This may call a paid provider. Returns the answer, observed token usage, estimated cost, and whether usage is simulated. It does not change the host agent's model.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in delegateInput) (*mcp.CallToolResult, delegateOutput, error) {
		var out delegateOutput
		base, err := gatewayURL(os.Getenv("RELAY_GATEWAY_URL"))
		if err != nil {
			return nil, out, err
		}
		key := os.Getenv("RELAY_KEY")
		if key == "" {
			return nil, out, errors.New("RELAY_KEY is required")
		}
		if in.Route == "" {
			in.Route = "chat"
		}
		if in.MaxTokens == 0 {
			in.MaxTokens = 256
		}
		c, err := delegate(ctx, base, key, in.Route, in.MaxTokens, in.Task, client)
		if err != nil {
			return nil, out, err
		}
		out = delegateOutput{
			Answer: c.Choices[0].Message.Content, RequestID: c.ID, Provider: c.Provider,
			Model: c.Model, Route: c.Route, InputTokens: c.Usage.Input,
			OutputTokens: c.Usage.Output, EstimatedCostNanoUSD: c.CostNanoUSD,
			Simulated: c.Usage.Simulated,
		}
		return nil, out, nil
	})
	return server
}

func serveMCP(ctx context.Context, client *http.Client) error {
	return newMCPServer(client).Run(ctx, &mcp.StdioTransport{})
}
