package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in: consumes normal Codex account usage. Never runs in ordinary tests/CI.
func TestLiveCodexCatalog(t *testing.T) {
	if os.Getenv("RELAY_LIVE_CODEX") != "1" {
		t.Skip("set RELAY_LIVE_CODEX=1 to use your authenticated Codex CLI")
	}
	t.Chdir(filepath.Join("..", ".."))
	models, err := discoverCodexModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type observation struct {
		Answer               string    `json:"answer"`
		ExpectedPackageFound bool      `json:"expected_package_found"`
		Run                  nativeRun `json:"run"`
	}
	proof := struct {
		CheckedAt string        `json:"checked_at"`
		Prompt    string        `json:"prompt"`
		Catalog   []codexModel  `json:"catalog"`
		Runs      []observation `json:"runs"`
	}{CheckedAt: time.Now().UTC().Format(time.RFC3339), Prompt: "Read the repository and identify the Go package implementing Relay's Redis token bucket. Respond only with its repository-relative directory path.", Catalog: models}
	levels := []string{"low", "medium", "high", "xhigh"}
	for i, model := range models {
		t.Run(model.Model, func(t *testing.T) {
			effort := model.DefaultEffort
			for _, supported := range model.Efforts {
				if supported.Effort == levels[i%len(levels)] {
					effort = supported.Effort
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			answer, run, err := runNativeTask(ctx, nativeTaskOptions{Host: "codex", Mode: "auto", Model: model.Model, Effort: effort, Prompt: proof.Prompt}, io.Discard)
			found := strings.Contains(answer, "internal/ratelimit")
			proof.Runs = append(proof.Runs, observation{Answer: answer, ExpectedPackageFound: found, Run: run})
			if err != nil {
				t.Errorf("live run: %v", err)
			}
			if !found || !run.Success || run.InputTokens == 0 || run.OutputTokens == 0 {
				t.Errorf("missing expected answer or usage: %+v", proof.Runs[len(proof.Runs)-1])
			}
			t.Logf("model=%s effort=%s input=%d cached=%d output=%d elapsed=%dms verified=%t", run.RequestedModel, run.ReasoningEffort, run.InputTokens, run.CachedInputTokens, run.OutputTokens, run.DurationMS, found)
		})
	}
	if path := os.Getenv("RELAY_LIVE_REPORT"); path != "" {
		data, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
