package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/provider"
)

const maxNativePromptBytes = 32 << 10

type nativeRun struct {
	ID                string   `json:"id"`
	StartedAt         string   `json:"started_at"`
	Host              string   `json:"host"`
	Tier              string   `json:"tier"`
	RoutingSource     string   `json:"routing_source"`
	ReasoningEffort   string   `json:"reasoning_effort,omitempty"`
	RequestedModel    string   `json:"requested_model"`
	ActualModel       string   `json:"actual_model,omitempty"`
	InputTokens       int64    `json:"input_tokens"`
	CachedInputTokens int64    `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int64    `json:"cache_write_tokens,omitempty"`
	OutputTokens      int64    `json:"output_tokens"`
	ReportedCostUSD   *float64 `json:"reported_cost_usd,omitempty"`
	DurationMS        int64    `json:"duration_ms"`
	Success           bool     `json:"success"`
}

func runNativeCLI(args []string, input io.Reader, output, errorOutput io.Writer) error {
	if args[0] == "models" {
		if len(args) != 1 {
			return errors.New("usage: relay-agent models")
		}
		return listCodexModels(context.Background(), output)
	}
	if args[0] == "runs" {
		return listNativeRuns(args[1:], output)
	}
	if args[0] == "serve" {
		return serveNativeRuns(args[1:], output)
	}
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", "codex", "codex or claude")
	model := fs.String("model", "", "explicit model from relay-agent models")
	effort := fs.String("effort", "auto", "auto or a supported reasoning effort")
	routing := fs.String("routing", "", "JSON mapping simple, standard, complex to model and effort")
	dryRun := fs.Bool("dry-run", false, "show the validated selection without running a model")
	mode := fs.String("mode", "auto", "auto, cheap, or strong")
	cheap := fs.String("cheap-model", "", "model for simple tasks")
	strong := fs.String("strong-model", "", "model for complex tasks")
	write := fs.Bool("write", false, "allow the native agent to edit files with its normal approval policy")
	logPath := fs.String("log", ".relay/runs.jsonl", "local metadata-only run log")
	timeout := fs.Duration("timeout", 10*time.Minute, "maximum run duration")
	if err := fs.Parse(args[1:]); err != nil || len(fs.Args()) != 0 {
		return errors.New("usage: relay-agent run --host codex|claude [--mode auto|cheap|strong] [--model ID] [--effort LEVEL] [--routing PATH] [--dry-run] [--cheap-model ID] [--strong-model ID] [--write] [--log PATH] < task.txt")
	}
	if *host != "codex" && *host != "claude" {
		return errors.New("host must be codex or claude")
	}
	if *timeout < time.Second || *timeout > time.Hour {
		return errors.New("timeout must be between 1 second and 1 hour")
	}
	promptBytes, err := io.ReadAll(io.LimitReader(input, maxNativePromptBytes+1))
	if err != nil {
		return fmt.Errorf("read task: %w", err)
	}
	prompt := strings.TrimSpace(string(promptBytes))
	if prompt == "" || len(promptBytes) > maxNativePromptBytes {
		return errors.New("task must be nonempty and at most 32 KiB")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	opts := nativeTaskOptions{Host: *host, Mode: *mode, CheapModel: *cheap, StrongModel: *strong, Model: *model, Effort: *effort, RoutingPath: *routing, Prompt: prompt, Write: *write, LogPath: *logPath}
	if *dryRun {
		if *host != "codex" {
			return errors.New("dry-run is supported for Codex only")
		}
		models, err := discoverCodexModels(ctx)
		if err != nil {
			return err
		}
		decision, err := selectCodexRoute(opts, models)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(decision)
	}
	answer, record, err := runNativeTask(ctx, opts, errorOutput)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "%s\n\n[Relay run %s: host=%s, selected=%s, effort=%s, tier=%s (%s), input=%d, cached=%d, output=%d, duration=%dms",
		answer, record.ID, record.Host, record.RequestedModel, record.ReasoningEffort, record.Tier, record.RoutingSource, record.InputTokens, record.CachedInputTokens, record.OutputTokens, record.DurationMS)
	if err != nil {
		return err
	}
	if record.ReportedCostUSD != nil {
		_, err = fmt.Fprintf(output, ", host-estimate=$%.6f", *record.ReportedCostUSD)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(output, "]")
	return err
}

type nativeTaskOptions struct {
	Host, Mode, CheapModel, StrongModel, Prompt, LogPath string
	Model, Effort, RoutingPath                           string
	Write                                                bool
}

func runNativeTask(ctx context.Context, opts nativeTaskOptions, errorOutput io.Writer) (string, nativeRun, error) {
	var record nativeRun
	if opts.Host != "codex" && opts.Host != "claude" {
		return "", record, errors.New("host must be codex or claude")
	}
	if strings.TrimSpace(opts.Prompt) == "" || len(opts.Prompt) > maxNativePromptBytes {
		return "", record, errors.New("task must be nonempty and at most 32 KiB")
	}
	var model, tier, source, effort string
	var err error
	if opts.Host == "codex" {
		models, discoverErr := discoverCodexModels(ctx)
		if discoverErr != nil {
			return "", record, discoverErr
		}
		decision, routeErr := selectCodexRoute(opts, models)
		if routeErr != nil {
			return "", record, routeErr
		}
		model, tier, source, effort = decision.Model, decision.Tier, decision.Source, decision.Effort
	} else {
		if opts.Model != "" || opts.RoutingPath != "" || (opts.Effort != "" && opts.Effort != "auto") {
			return "", record, errors.New("model, effort, and routing options require Codex")
		}
		model, tier, source, err = selectClaudeModel(opts.Mode, opts.CheapModel, opts.StrongModel, opts.Prompt)
		if err != nil {
			return "", record, err
		}
	}
	command, commandArgs := nativeCommand(opts.Host, model, effort, opts.Write)
	if _, err := exec.LookPath(command); err != nil {
		return "", record, fmt.Errorf("%s CLI is not installed or not on PATH", command)
	}
	cmd := exec.CommandContext(ctx, command, commandArgs...)
	cmd.Env = nativeChildEnv()
	cmd.Stdin = strings.NewReader(opts.Prompt)
	cmd.Stderr = errorOutput
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", record, err
	}
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return "", record, err
	}
	record = nativeRun{ID: hex.EncodeToString(idBytes), StartedAt: time.Now().UTC().Format(time.RFC3339), Host: opts.Host, Tier: tier, RoutingSource: source, RequestedModel: model, ReasoningEffort: effort}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return "", record, fmt.Errorf("start %s: %w", command, err)
	}
	var answer string
	if opts.Host == "codex" {
		answer, err = parseCodexEvents(stdout, &record)
	} else {
		answer, err = parseClaudeResult(stdout, &record)
	}
	if err != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	record.DurationMS = time.Since(start).Milliseconds()
	record.Success = err == nil && waitErr == nil && answer != ""
	if opts.LogPath != "" {
		if logErr := appendNativeRun(opts.LogPath, record); logErr != nil {
			return "", record, fmt.Errorf("save run metadata: %w", logErr)
		}
	}
	if err != nil {
		return "", record, fmt.Errorf("read %s result: %w", opts.Host, err)
	}
	if waitErr != nil {
		return "", record, fmt.Errorf("%s exited unsuccessfully: %w", opts.Host, waitErr)
	}
	if answer == "" {
		return "", record, fmt.Errorf("%s returned no final answer", opts.Host)
	}
	return answer, record, nil
}

func selectClaudeModel(mode, cheap, strong, prompt string) (model, tier, source string, err error) {
	if mode != "auto" && mode != "cheap" && mode != "strong" {
		return "", "", "", errors.New("mode must be auto, cheap, or strong")
	}
	if cheap == "" {
		cheap = "haiku"
	}
	if strong == "" {
		strong = "sonnet"
	}
	for _, name := range []string{cheap, strong} {
		if name == "" || len(name) > 100 || strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t\r\n") {
			return "", "", "", errors.New("model IDs must be nonempty names without whitespace or leading dashes")
		}
	}
	if mode == "cheap" {
		return cheap, "simple", "explicit", nil
	}
	if mode == "strong" {
		return strong, "complex", "explicit", nil
	}
	r, err := (classifier.Local{}).Classify(context.Background(), provider.Request{Messages: []provider.Message{{Role: "user", Content: prompt}}})
	if err != nil {
		return "", "", "", err
	}
	if r.Tier == "complex" {
		return strong, r.Tier, "local", nil
	}
	return cheap, r.Tier, "local", nil
}

func nativeCommand(host, model, effort string, write bool) (string, []string) {
	if host == "codex" {
		args := []string{"exec", "--json", "--ephemeral", "--model", model}
		if effort != "" {
			args = append(args, "-c", "model_reasoning_effort="+fmt.Sprintf("%q", effort))
		}
		if write {
			args = append(args, "--approve-for-me")
		} else {
			args = append(args, "--sandbox", "read-only")
		}
		return "codex", append(args, "-")
	}
	args := []string{"-p", "--output-format", "json", "--model", model}
	if !write {
		args = append(args, "--permission-mode", "plan")
	}
	return "claude", args
}

func appendNativeRun(path string, record nativeRun) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}

func listNativeRuns(args []string, output io.Writer) error {
	fs := flag.NewFlagSet("runs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("log", ".relay/runs.jsonl", "local run log")
	if err := fs.Parse(args); err != nil || len(fs.Args()) != 0 {
		return errors.New("usage: relay-agent runs [--log PATH]")
	}
	file, err := os.Open(*path)
	if errors.Is(err, os.ErrNotExist) {
		_, err = fmt.Fprintln(output, "No native agent runs recorded yet.")
		return err
	}
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 8<<20))
	_, _ = fmt.Fprintln(output, "STARTED (UTC)          HOST    TIER      MODEL             EFFORT  INPUT  CACHED  OUTPUT  STATUS")
	for {
		var r nativeRun
		if err := decoder.Decode(&r); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		status := "error"
		if r.Success {
			status = "ok"
		}
		if _, err := fmt.Fprintf(output, "%-22s %-7s %-9s %-17s %-7s %6d %7d %7d  %s\n", r.StartedAt, r.Host, r.Tier, r.RequestedModel, r.ReasoningEffort, r.InputTokens, r.CachedInputTokens, r.OutputTokens, status); err != nil {
			return err
		}
	}
}

func nativeChildEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "RELAY_KEY=") || strings.HasPrefix(entry, "JEV_API_KEY=") || strings.HasPrefix(entry, "RELAY_NATIVE_CHILD=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "RELAY_NATIVE_CHILD=1")
}
