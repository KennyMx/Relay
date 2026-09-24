package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Read the same picker-visible catalog that Codex exposes, never credentials.
type codexModel struct {
	ID            string `json:"id"`
	Model         string `json:"model"`
	DisplayName   string `json:"displayName"`
	DefaultEffort string `json:"defaultReasoningEffort"`
	Efforts       []struct {
		Effort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
	IsDefault bool `json:"isDefault"`
}

func discoverCodexModels(ctx context.Context) ([]codexModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "codex", "app-server")
	cmd.Env = nativeChildEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Codex catalog: %w", err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	models, err := readCodexCatalog(stdin, stdout)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("Codex model discovery: %w", ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("Codex model discovery: %w", err)
	}
	return models, nil
}

func readCodexCatalog(input io.Writer, output io.Reader) ([]codexModel, error) {
	encoder := json.NewEncoder(input)
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	call := func(id int, method string, params any, result any) error {
		if err := encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			return err
		}
		for scanner.Scan() {
			var response struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
				return err
			}
			if response.ID == nil || *response.ID != id {
				continue
			}
			if response.Error != nil {
				return errors.New(response.Error.Message)
			}
			return json.Unmarshal(response.Result, result)
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		return io.ErrUnexpectedEOF
	}
	var init json.RawMessage
	if err := call(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "relay", "version": "0.4.0"}}, &init); err != nil {
		return nil, err
	}
	if err := encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	var models []codexModel
	var cursor *string
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		var result struct {
			Data       []codexModel `json:"data"`
			NextCursor *string      `json:"nextCursor"`
		}
		if err := call(page+2, "model/list", map[string]any{"limit": 100, "includeHidden": false, "cursor": cursor}, &result); err != nil {
			return nil, err
		}
		for _, model := range result.Data {
			if model.Model == "" {
				return nil, errors.New("catalog contains an empty model ID")
			}
			models = append(models, model)
		}
		if result.NextCursor == nil {
			if len(models) == 0 {
				return nil, errors.New("Codex returned no available models; check your Codex login")
			}
			return models, nil
		}
		if seen[*result.NextCursor] {
			return nil, errors.New("catalog repeated a pagination cursor")
		}
		seen[*result.NextCursor] = true
		cursor = result.NextCursor
	}
	return nil, errors.New("catalog exceeded pagination limit")
}

func listCodexModels(ctx context.Context, output io.Writer) error {
	models, err := discoverCodexModels(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(models)
}
