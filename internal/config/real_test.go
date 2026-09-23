package config

import (
	"strings"
	"testing"
)

func realEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"RELAY_PRIMARY_PROVIDER", "RELAY_PRIMARY_MODEL", "RELAY_PRIMARY_INPUT_USD_PER_M", "RELAY_PRIMARY_OUTPUT_USD_PER_M",
		"RELAY_FALLBACK_PROVIDER", "RELAY_FALLBACK_MODEL", "RELAY_FALLBACK_INPUT_USD_PER_M", "RELAY_FALLBACK_OUTPUT_USD_PER_M",
		"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "COHERE_API_KEY", "RELAY_CLASSIFIER",
	} {
		t.Setenv(name, "")
	}
}

func TestRealFromEnv(t *testing.T) {
	realEnv(t)
	t.Setenv("RELAY_PRIMARY_PROVIDER", "openai")
	t.Setenv("RELAY_PRIMARY_MODEL", "account-model")
	t.Setenv("RELAY_PRIMARY_INPUT_USD_PER_M", "0.15")
	t.Setenv("RELAY_PRIMARY_OUTPUT_USD_PER_M", "0.60")

	c, err := RealFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultRoute != "chat" || c.MaxAttempts != 1 || len(c.Routes["chat"].Targets) != 1 || c.Pricing["openai/account-model"].Input != 150 || c.Pricing["openai/account-model"].Output != 600 {
		t.Fatalf("unexpected single-provider configuration: %+v", c)
	}
	if _, err := c.BuildRouter(); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "local-fixture-key")
	r, err := c.BuildRouter()
	if err != nil {
		t.Fatal(err)
	}
	targets, err := r.Select("", "")
	if err != nil || len(targets) != 1 || targets[0].Provider != "openai" {
		t.Fatalf("unexpected route: %+v, %v", targets, err)
	}

	t.Setenv("RELAY_FALLBACK_PROVIDER", "anthropic")
	t.Setenv("RELAY_FALLBACK_MODEL", "backup-model")
	t.Setenv("RELAY_FALLBACK_INPUT_USD_PER_M", "3")
	t.Setenv("RELAY_FALLBACK_OUTPUT_USD_PER_M", "15")
	c, err = RealFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxAttempts != 2 || len(c.Routes["chat"].Targets) != 2 || c.Routes["chat"].Targets[1].Provider != "anthropic" {
		t.Fatalf("unexpected fallback configuration: %+v", c)
	}
	t.Setenv("ANTHROPIC_API_KEY", "local-fixture-key")
	if _, err := c.BuildRouter(); err != nil {
		t.Fatal(err)
	}
}

func TestRealFromEnvRejectsInvalidSettings(t *testing.T) {
	tests := []struct {
		name, key, value string
	}{
		{"missing primary", "RELAY_PRIMARY_PROVIDER", ""},
		{"unsupported provider", "RELAY_PRIMARY_PROVIDER", "mock"},
		{"placeholder model", "RELAY_PRIMARY_MODEL", "YOUR_MODEL_ID"},
		{"readme placeholder", "RELAY_PRIMARY_MODEL", "<your-model-id>"},
		{"missing price", "RELAY_PRIMARY_INPUT_USD_PER_M", ""},
		{"fractional precision", "RELAY_PRIMARY_INPUT_USD_PER_M", "0.0001"},
		{"fraction notation", "RELAY_PRIMARY_INPUT_USD_PER_M", "1/2"},
		{"negative price", "RELAY_PRIMARY_INPUT_USD_PER_M", "-1"},
		{"partial fallback", "RELAY_FALLBACK_PROVIDER", "anthropic"},
		{"same fallback", "RELAY_FALLBACK_PROVIDER", "openai"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			realEnv(t)
			t.Setenv("RELAY_PRIMARY_PROVIDER", "openai")
			t.Setenv("RELAY_PRIMARY_MODEL", "account-model")
			t.Setenv("RELAY_PRIMARY_INPUT_USD_PER_M", "0.15")
			t.Setenv("RELAY_PRIMARY_OUTPUT_USD_PER_M", "0.60")
			t.Setenv(tt.key, tt.value)
			if _, err := RealFromEnv(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}
