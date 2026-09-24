package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPDelegateTask(t *testing.T) {
	t.Setenv("RELAY_KEY", "fixture-key")
	t.Setenv("RELAY_GATEWAY_URL", "http://127.0.0.1:8080")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer(fakeDoer(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing gateway key")
		}
		return reply(200, `{"id":"req-1","model":"mock-fast","provider":"mock","route":"chat","choices":[{"message":{"content":"Done."}}],"usage":{"input_tokens":5,"output_tokens":2,"simulated":true},"cost_nano_usd":1000}`), nil
	}))
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "delegate_task", Arguments: map[string]any{"task": "Summarize mutexes."}})
	if err != nil || result.IsError {
		t.Fatalf("tool call: %v %+v", err, result)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out delegateOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Answer != "Done." || !out.Simulated || out.InputTokens != 5 || out.RequestID != "req-1" {
		t.Fatalf("incorrect result: %+v", out)
	}
}

func TestMCPRejectsMissingKey(t *testing.T) {
	t.Setenv("RELAY_KEY", "")
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer(fakeDoer(func(*http.Request) (*http.Response, error) { t.Error("unexpected network call"); return nil, nil }))
	ctx := context.Background()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "delegate_task", Arguments: map[string]any{"task": "hello"}})
	if err != nil || !result.IsError {
		t.Fatalf("expected tool error: %v %+v", err, result)
	}
}
