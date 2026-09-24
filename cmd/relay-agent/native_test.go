package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeModelRouting(t *testing.T) {
	model, tier, source, err := selectNativeModel("codex", "auto", "", "", "Summarize these notes.")
	if err != nil || model != "gpt-6-luna" || tier != "simple" || source != "local" {
		t.Fatalf("simple route: %s %s %s %v", model, tier, source, err)
	}
	model, tier, _, err = selectNativeModel("codex", "auto", "", "", "Review distributed architecture tradeoffs.")
	if err != nil || model != "gpt-6-sol" || tier != "complex" {
		t.Fatalf("complex route: %s %s %v", model, tier, err)
	}
	model, _, _, err = selectNativeModel("claude", "cheap", "", "", "Debug this race condition")
	if err != nil || model != "haiku" {
		t.Fatalf("explicit route: %s %v", model, err)
	}
	if _, _, _, err := selectNativeModel("codex", "cheap", "--bad", "", "hi"); err == nil {
		t.Fatal("accepted flag-like model")
	}
}

func TestNativeCodexCommandKeepsReadOnlyDefault(t *testing.T) {
	_, readArgs := nativeCommand("codex", "gpt-6-luna", false)
	if !strings.Contains(strings.Join(readArgs, " "), "--sandbox read-only") || strings.Contains(strings.Join(readArgs, " "), "--approve-for-me") {
		t.Fatalf("read-only args: %v", readArgs)
	}
	_, writeArgs := nativeCommand("codex", "gpt-6-luna", true)
	if !strings.Contains(strings.Join(writeArgs, " "), "--approve-for-me") || strings.Contains(strings.Join(writeArgs, " "), "--sandbox") {
		t.Fatalf("write args: %v", writeArgs)
	}
}

func TestNativeParsers(t *testing.T) {
	var codex nativeRun
	answer, err := parseCodexEvents(strings.NewReader(""+
		`{"type":"item.completed","item":{"type":"agent_message","text":"READY"}}`+"\n"+
		`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":3}}`+"\n"), &codex)
	if err != nil || answer != "READY" || codex.InputTokens != 100 || codex.CachedInputTokens != 80 || codex.OutputTokens != 3 {
		t.Fatalf("Codex: %q %+v %v", answer, codex, err)
	}
	var claude nativeRun
	answer, err = parseClaudeResult(strings.NewReader(`{"type":"result","is_error":false,"result":"DONE","total_cost_usd":0.001,"usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"output_tokens":7},"modelUsage":{"claude-haiku":{"costUSD":0.001}}}`), &claude)
	if err != nil || answer != "DONE" || claude.InputTokens != 35 || claude.OutputTokens != 7 || claude.ActualModel != "claude-haiku" || claude.ReportedCostUSD == nil {
		t.Fatalf("Claude: %q %+v %v", answer, claude, err)
	}
	if _, err := parseCodexEvents(strings.NewReader(`{"type":"turn.failed"}`), &codex); err == nil {
		t.Fatal("accepted failed Codex turn")
	}
}

func TestNativeLogOmitsPromptAndAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	r := nativeRun{ID: "id", Host: "codex", RequestedModel: "gpt-6-luna", InputTokens: 42, Success: true}
	if err := appendNativeRun(path, r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"input_tokens":42`)) || bytes.Contains(data, []byte("secret task")) {
		t.Fatal("incorrect log")
	}
	var output bytes.Buffer
	if err := listNativeRuns([]string{"--log", path}, &output); err != nil || !strings.Contains(output.String(), "gpt-6-luna") {
		t.Fatalf("runs: %s %v", output.String(), err)
	}
}

func TestNativeRunsPageUsesRecordedDataAndEscapesFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	if err := appendNativeRun(path, nativeRun{Host: "codex", RequestedModel: "<script>alert(1)</script>", InputTokens: 42, Success: true}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	nativeRunsHandler(path).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "&lt;script&gt;") || strings.Contains(r.Body.String(), "<script>alert(1)</script>") || !strings.Contains(r.Body.String(), ">42<") {
		t.Fatalf("incorrect page: %d %s", r.Code, r.Body.String())
	}
	r = httptest.NewRecorder()
	nativeRunsHandler(path).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/secret", nil))
	if r.Code != 404 {
		t.Fatalf("unexpected route: %d", r.Code)
	}
	r = httptest.NewRecorder()
	nativeRunsHandler(path).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "http://attacker.example/", nil))
	if r.Code != http.StatusForbidden {
		t.Fatalf("untrusted host: %d", r.Code)
	}
}
