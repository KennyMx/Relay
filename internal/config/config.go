package config

import (
	"encoding/json"
	"fmt"
	"github.com/KennyMx/Relay/internal/pricing"
	"github.com/KennyMx/Relay/internal/provider"
	"github.com/KennyMx/Relay/internal/router"
	"io"
	"os"
	"strings"
	"time"
)

type Provider struct {
	Kind     string `json:"kind"`
	Scenario string `json:"scenario,omitempty"`
}
type Config struct {
	DefaultRoute     string                  `json:"default_route"`
	RequestTimeoutMS int                     `json:"request_timeout_ms"`
	AttemptTimeoutMS int                     `json:"attempt_timeout_ms"`
	MaxAttempts      int                     `json:"max_attempts"`
	Providers        map[string]Provider     `json:"providers"`
	Routes           map[string]router.Route `json:"routes"`
	Pricing          pricing.Table           `json:"pricing"`
}

func Load(path string) (Config, error) {
	var c Config
	f, err := os.Open(path)
	if err != nil {
		return c, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return c, fmt.Errorf("configuration must contain one JSON object")
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.RequestTimeoutMS < 1 || c.RequestTimeoutMS > 120000 || c.AttemptTimeoutMS < 1 || c.AttemptTimeoutMS > c.RequestTimeoutMS || c.MaxAttempts < 1 || c.MaxAttempts > 5 {
		return fmt.Errorf("invalid timeout or attempt limits")
	}
	if _, ok := c.Routes[c.DefaultRoute]; !ok {
		return fmt.Errorf("default route missing")
	}
	for name, p := range c.Providers {
		if name == "" {
			return fmt.Errorf("empty provider name")
		}
		switch p.Kind {
		case "mock":
			switch p.Scenario {
			case "", "success", "429", "500", "timeout":
			default:
				return fmt.Errorf("invalid mock scenario")
			}
		case "openai", "anthropic", "cohere":
		default:
			return fmt.Errorf("unknown provider kind %q", p.Kind)
		}
	}
	for name, r := range c.Routes {
		if strings.TrimSpace(name) == "" || len(r.Targets) == 0 || len(r.Targets) > 5 {
			return fmt.Errorf("invalid route %q", name)
		}
		seen := map[string]bool{}
		for _, t := range r.Targets {
			if _, ok := c.Providers[t.Provider]; !ok || t.Model == "" {
				return fmt.Errorf("invalid target in %q", name)
			}
			if seen[t.Provider] {
				return fmt.Errorf("duplicate provider in %q", name)
			}
			seen[t.Provider] = true
			rate, ok := c.Pricing[t.Provider+"/"+t.Model]
			if !ok || rate.Input < 0 || rate.Output < 0 || rate.Input > 1_000_000_000 || rate.Output > 1_000_000_000 {
				return fmt.Errorf("missing or invalid price for %s/%s", t.Provider, t.Model)
			}
		}
	}
	return nil
}
func (c Config) BuildRouter() (*router.Router, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	providers := make(map[string]provider.Provider)
	for name, p := range c.Providers {
		if p.Kind == "mock" {
			providers[name] = provider.Mock{Scenario: p.Scenario}
			continue
		}
		key := os.Getenv(strings.ToUpper(p.Kind) + "_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("%s_API_KEY required for configured provider %s", strings.ToUpper(p.Kind), name)
		}
		h := provider.HTTP{APIKey: key}
		switch p.Kind {
		case "openai":
			h.BaseURL = "https://api.openai.com"
			providers[name] = provider.OpenAI{HTTP: h}
		case "anthropic":
			h.BaseURL = "https://api.anthropic.com"
			providers[name] = provider.Anthropic{HTTP: h}
		case "cohere":
			h.BaseURL = "https://api.cohere.com"
			providers[name] = provider.Cohere{HTTP: h}
		}
	}
	return &router.Router{Providers: providers, Routes: c.Routes, Default: c.DefaultRoute, AttemptTimeout: time.Duration(c.AttemptTimeoutMS) * time.Millisecond, MaxAttempts: c.MaxAttempts}, nil
}
