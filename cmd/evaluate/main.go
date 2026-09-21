// evaluate runs a small, versioned routing evaluation. Live Jev is explicit opt-in.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/config"
	"github.com/KennyMx/Relay/internal/provider"
)

type example struct {
	ID       string `json:"id"`
	Prompt   string `json:"prompt"`
	Expected string `json:"expected"`
}
type observation struct {
	ID             string             `json:"id"`
	Expected       string             `json:"expected"`
	Selected       string             `json:"selected"`
	Reason         string             `json:"reason"`
	Classification *classifier.Result `json:"classification"`
	WallMS         float64            `json:"wall_ms"`
	SimulatedCost  int64              `json:"simulated_completion_cost_nano_usd"`
	BaselineCost   int64              `json:"simulated_all_reasoning_cost_nano_usd"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	live := flag.Bool("live", false, "Call Jev once per case; consumes API credits. Never calls a paid completion provider.")
	casesPath := flag.String("cases", "docs/benchmarks/cases.json", "Evaluation corpus")
	configPath := flag.String("config", "config/relay.json", "Routing configuration")
	flag.Parse()
	// #nosec G304 -- trusted CLI path, not remotely supplied.
	data, err := os.ReadFile(*casesPath)
	if err != nil {
		return err
	}
	var cases []example
	if err = json.Unmarshal(data, &cases); err != nil {
		return err
	}
	if len(cases) < 1 || len(cases) > 50 {
		return fmt.Errorf("evaluation must contain 1-50 cases")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	for _, p := range cfg.Providers {
		if p.Kind != "mock" {
			return fmt.Errorf("evaluation requires mock-only completion providers")
		}
	}
	mode := "local"
	if *live {
		mode = "jev"
	}
	if err = os.Setenv("RELAY_CLASSIFIER", mode); err != nil {
		return err
	}
	r, err := cfg.BuildRouter()
	if err != nil {
		return err
	}
	if r.Auto == nil {
		return fmt.Errorf("auto_routing required")
	}
	out := struct {
		Timestamp   string        `json:"timestamp"`
		Mode        string        `json:"mode"`
		Environment string        `json:"environment"`
		Corpus      string        `json:"corpus"`
		Cases       int           `json:"cases"`
		Agreement   int           `json:"agreement_count"`
		P50         float64       `json:"p50_wall_ms"`
		P95         float64       `json:"p95_wall_ms"`
		Records     []observation `json:"records"`
	}{Timestamp: time.Now().UTC().Format(time.RFC3339), Mode: mode, Environment: runtime.GOOS + "/" + runtime.GOARCH + " " + runtime.Version(), Corpus: *casesPath, Cases: len(cases)}
	timings := make([]float64, 0, len(cases))
	for _, c := range cases {
		if !classifier.ValidTier(c.Expected) {
			return fmt.Errorf("invalid reference tier in evaluation corpus")
		}
		req := provider.Request{Messages: []provider.Message{{Role: "user", Content: c.Prompt}}, MaxTokens: 256}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.RequestTimeoutMS)*time.Millisecond)
		start := time.Now()
		targets, d, err := r.Resolve(ctx, "auto", "", req)
		wall := float64(time.Since(start).Microseconds()) / 1000
		if err != nil {
			cancel()
			return err
		}
		result, attempts, err := r.Complete(ctx, targets, req)
		cancel()
		if err != nil {
			return err
		}
		last := attempts[len(attempts)-1]
		cost, err := cfg.Pricing.Estimate(last.Provider, last.Model, result.Usage)
		if err != nil {
			return err
		}
		baseline := cfg.Routes[cfg.AutoRouting.Routes["complex"]].Targets[0]
		high, err := cfg.Pricing.Estimate(baseline.Provider, baseline.Model, result.Usage)
		if err != nil {
			return err
		}
		if d.Route == cfg.AutoRouting.Routes[c.Expected] {
			out.Agreement++
		}
		timings = append(timings, wall)
		out.Records = append(out.Records, observation{c.ID, c.Expected, d.Route, d.Reason, d.Classification, wall, cost, high})
	}
	sort.Float64s(timings)
	out.P50 = timings[(len(timings)-1)/2]
	out.P95 = timings[(95*len(timings)+99)/100-1]
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
