package main

import (
	"encoding/json"
	"errors"
	"io"
)

func parseCodexEvents(input io.Reader, record *nativeRun) (string, error) {
	decoder := json.NewDecoder(io.LimitReader(input, 64<<20))
	var answer string
	completed := false
	for {
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage struct {
				Input      int64 `json:"input_tokens"`
				Cached     int64 `json:"cached_input_tokens"`
				CacheWrite int64 `json:"cache_write_input_tokens"`
				Output     int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return "", err
		}
		switch event.Type {
		case "item.completed":
			if event.Item.Type == "agent_message" {
				answer = event.Item.Text
			}
		case "turn.completed":
			completed = true
			record.InputTokens = event.Usage.Input
			record.CachedInputTokens = event.Usage.Cached
			record.CacheWriteTokens = event.Usage.CacheWrite
			record.OutputTokens = event.Usage.Output
		case "turn.failed":
			return "", errors.New("Codex turn failed")
		}
	}
	if !completed {
		return "", errors.New("Codex did not report a completed turn")
	}
	if record.InputTokens < 0 || record.OutputTokens < 0 || record.CachedInputTokens < 0 || record.CacheWriteTokens < 0 {
		return "", errors.New("invalid Codex usage")
	}
	return answer, nil
}

func parseClaudeResult(input io.Reader, record *nativeRun) (string, error) {
	var result struct {
		Type         string   `json:"type"`
		IsError      bool     `json:"is_error"`
		Result       string   `json:"result"`
		TotalCostUSD *float64 `json:"total_cost_usd"`
		Usage        struct {
			Input         int64 `json:"input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			Output        int64 `json:"output_tokens"`
		} `json:"usage"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 16<<20)).Decode(&result); err != nil {
		return "", err
	}
	if result.Type != "result" || result.IsError {
		return "", errors.New("Claude Code did not return a successful result")
	}
	record.InputTokens = result.Usage.Input + result.Usage.CacheRead + result.Usage.CacheCreation
	record.CachedInputTokens = result.Usage.CacheRead
	record.CacheWriteTokens = result.Usage.CacheCreation
	record.OutputTokens = result.Usage.Output
	record.ReportedCostUSD = result.TotalCostUSD
	if len(result.ModelUsage) == 1 {
		for name := range result.ModelUsage {
			record.ActualModel = name
		}
	}
	if len(result.ModelUsage) > 1 {
		record.ActualModel = "multiple"
	}
	if record.InputTokens < 0 || record.OutputTokens < 0 {
		return "", errors.New("invalid Claude usage")
	}
	return result.Result, nil
}
