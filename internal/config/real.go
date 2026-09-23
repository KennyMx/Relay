package config

import (
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/KennyMx/Relay/internal/pricing"
	"github.com/KennyMx/Relay/internal/router"
)

// RealFromEnv builds a minimal text-chat gateway from provider and model choices.
// Provider credentials remain in their existing *_API_KEY environment variables.
func RealFromEnv() (Config, error) {
	c := Config{
		DefaultRoute:     "chat",
		RequestTimeoutMS: 30000,
		AttemptTimeoutMS: 12000,
		Providers:        make(map[string]Provider),
		Routes:           make(map[string]router.Route),
		Pricing:          make(pricing.Table),
	}

	targets := make([]router.Target, 0, 2)
	for _, label := range []string{"PRIMARY", "FALLBACK"} {
		prefix := "RELAY_" + label + "_"
		name := strings.ToLower(strings.TrimSpace(os.Getenv(prefix + "PROVIDER")))
		model := strings.TrimSpace(os.Getenv(prefix + "MODEL"))
		input := strings.TrimSpace(os.Getenv(prefix + "INPUT_USD_PER_M"))
		output := strings.TrimSpace(os.Getenv(prefix + "OUTPUT_USD_PER_M"))
		if label == "FALLBACK" && name == "" && model == "" && input == "" && output == "" {
			continue
		}
		if name != "openai" && name != "anthropic" && name != "cohere" {
			return Config{}, fmt.Errorf("%sPROVIDER must be openai, anthropic, or cohere", prefix)
		}
		if model == "" || strings.HasPrefix(model, "YOUR_") || strings.ContainsAny(model, "<> \t\r\n") {
			return Config{}, fmt.Errorf("%sMODEL must be a provider model ID", prefix)
		}
		if _, exists := c.Providers[name]; exists {
			return Config{}, fmt.Errorf("primary and fallback must use different providers")
		}
		inputRate, err := dollarsPerMillion(input)
		if err != nil {
			return Config{}, fmt.Errorf("%sINPUT_USD_PER_M: %w", prefix, err)
		}
		outputRate, err := dollarsPerMillion(output)
		if err != nil {
			return Config{}, fmt.Errorf("%sOUTPUT_USD_PER_M: %w", prefix, err)
		}
		c.Providers[name] = Provider{Kind: name}
		targets = append(targets, router.Target{Provider: name, Model: model})
		c.Pricing[name+"/"+model] = pricing.Rate{Input: inputRate, Output: outputRate}
	}
	c.MaxAttempts = len(targets)
	c.Routes["chat"] = router.Route{Targets: targets}
	return c, c.Validate()
}

// One dollar per million tokens is 1,000 nano-USD per token. Reject prices
// finer than the ledger can represent instead of silently rounding them.
func dollarsPerMillion(value string) (int64, error) {
	if value == "" || strings.Trim(value, "0123456789.") != "" || strings.Count(value, ".") > 1 || strings.Trim(value, ".") == "" {
		return 0, fmt.Errorf("provide a nonnegative decimal price in USD per million tokens")
	}
	rate, ok := new(big.Rat).SetString(value)
	if !ok || rate.Sign() < 0 {
		return 0, fmt.Errorf("provide a nonnegative decimal price in USD per million tokens")
	}
	rate.Mul(rate, big.NewRat(1000, 1))
	if !rate.IsInt() || !rate.Num().IsInt64() || rate.Num().Int64() > 1_000_000_000 {
		return 0, fmt.Errorf("price must be a multiple of $0.001 per million tokens and within the supported range")
	}
	return rate.Num().Int64(), nil
}
