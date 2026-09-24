package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

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

type codexTaskInput struct {
	Task        string `json:"task" jsonschema:"A bounded coding task for a fresh, read-only Codex run. Do not include secrets."`
	Mode        string `json:"mode,omitempty" jsonschema:"Model route: auto, cheap, or strong. Defaults to cheap."`
	CheapModel  string `json:"cheap_model,omitempty" jsonschema:"Optional Codex model ID for cheap tasks."`
	StrongModel string `json:"strong_model,omitempty" jsonschema:"Optional Codex model ID for complex tasks."`
}

type codexTaskOutput struct {
	Answer string    `json:"answer"`
	Run    nativeRun `json:"run"`
}

func newMCPServer(client httpDoer) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "relay", Version: "0.2.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "route_codex_task",
		Description: "Launch one opt-in read-only Codex CLI subtask on a selected model using the user's existing Codex login. Returns its real answer and host-reported token usage. Does not change the current session model or make a provider API call directly. Use only when the user asks to route a bounded task through Relay.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in codexTaskInput) (*mcp.CallToolResult, codexTaskOutput, error) {
		var out codexTaskOutput
		if os.Getenv("RELAY_NATIVE_CHILD") == "1" {
			return nil, out, errors.New("nested Relay routing is disabled")
		}
		if len(in.Task) > maxPromptBytes {
			return nil, out, errors.New("task exceeds MCP limit of 8 KiB")
		}
		if in.Mode == "" {
			in.Mode = "cheap"
		}
		ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		answer, record, err := runNativeTask(ctx, nativeTaskOptions{Host: "codex", Mode: in.Mode, CheapModel: in.CheapModel, StrongModel: in.StrongModel, Prompt: in.Task, LogPath: ".relay/runs.jsonl"}, io.Discard)
		if err != nil {
			return nil, out, err
		}
		out = codexTaskOutput{Answer: answer, Run: record}
		return nil, out, nil
	})
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
