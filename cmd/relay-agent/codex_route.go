package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/KennyMx/Relay/internal/classifier"
	"github.com/KennyMx/Relay/internal/provider"
)

type modelRoute struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type codexDecision struct {
	modelRoute
	Tier   string `json:"tier"`
	Source string `json:"source"`
}

func selectCodexRoute(opts nativeTaskOptions, models []codexModel) (codexDecision, error) {
	var decision codexDecision
	if opts.Mode != "auto" && opts.Mode != "cheap" && opts.Mode != "strong" {
		return decision, errors.New("mode must be auto, cheap, or strong")
	}
	if len(models) == 0 {
		return decision, errors.New("no Codex models available")
	}
	routes := map[string]modelRoute{
		"simple":   {Model: "gpt-6-luna", Effort: "low"},
		"standard": {Model: "gpt-6-sol", Effort: "medium"},
		"complex":  {Model: "gpt-6-astra", Effort: "high"},
	}
	if opts.RoutingPath != "" {
		f, err := os.Open(opts.RoutingPath)
		if err != nil {
			return decision, fmt.Errorf("read routing config: %w", err)
		}
		defer f.Close()
		decoder := json.NewDecoder(io.LimitReader(f, 64<<10))
		decoder.DisallowUnknownFields()
		routes = nil
		if err := decoder.Decode(&routes); err != nil {
			return decision, fmt.Errorf("invalid routing config: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return decision, errors.New("routing config must contain one JSON object")
		}
		if len(routes) != 3 {
			return decision, errors.New("routing config requires simple, standard, and complex routes")
		}
		for _, tier := range []string{"simple", "standard", "complex"} {
			route, ok := routes[tier]
			if !ok || route.Model == "" {
				return decision, fmt.Errorf("routing config requires %s.model", tier)
			}
			if _, err := validateModelRoute(route, models); err != nil {
				return decision, fmt.Errorf("%s route: %w", tier, err)
			}
		}
	} else {
		// If an account lacks a preferred default, choose its advertised default.
		// Explicit model choices and user routing files never silently fall back.
		fallback := models[0]
		for _, model := range models {
			if model.IsDefault {
				fallback = model
				break
			}
		}
		for tier, route := range routes {
			found := false
			for _, model := range models {
				if model.Model == route.Model {
					found = true
					break
				}
			}
			if !found {
				routes[tier] = modelRoute{Model: fallback.Model, Effort: fallback.DefaultEffort}
			}
		}
	}
	if opts.CheapModel != "" {
		r := routes["simple"]
		r.Model = opts.CheapModel
		r.Effort = ""
		routes["simple"] = r
	}
	if opts.StrongModel != "" {
		r := routes["complex"]
		r.Model = opts.StrongModel
		r.Effort = ""
		routes["complex"] = r
	}
	decision.Source = "explicit"
	if opts.Model != "" {
		decision.Tier = "manual"
		decision.modelRoute = modelRoute{Model: opts.Model, Effort: opts.Effort}
	} else {
		switch opts.Mode {
		case "cheap":
			decision.Tier = "simple"
		case "strong":
			decision.Tier = "complex"
		default:
			r, err := (classifier.Local{}).Classify(context.Background(), provider.Request{Messages: []provider.Message{{Role: "user", Content: opts.Prompt}}})
			if err != nil {
				return decision, err
			}
			decision.Tier, decision.Source = r.Tier, "local"
		}
		decision.modelRoute = routes[decision.Tier]
		if opts.RoutingPath != "" {
			decision.Source += "+config"
		}
		if opts.Effort != "" && opts.Effort != "auto" {
			decision.Effort = opts.Effort
		}
	}
	route, err := validateModelRoute(decision.modelRoute, models)
	decision.modelRoute = route
	return decision, err
}

func validateModelRoute(route modelRoute, models []codexModel) (modelRoute, error) {
	for _, model := range models {
		if model.Model != route.Model {
			continue
		}
		switch strings.ToLower(route.Effort) {
		case "", "auto":
			route.Effort = model.DefaultEffort
		case "light":
			route.Effort = "low"
		case "extra-high", "extrahigh", "extra high":
			route.Effort = "xhigh"
		default:
			route.Effort = strings.ToLower(route.Effort)
		}
		for _, effort := range model.Efforts {
			if effort.Effort == route.Effort {
				return route, nil
			}
		}
		// Some non-reasoning models advertise no effort setting.
		if route.Effort == "" && len(model.Efforts) == 0 {
			return route, nil
		}
		supported := []string{}
		for _, effort := range model.Efforts {
			supported = append(supported, effort.Effort)
		}
		return route, fmt.Errorf("model %s does not support effort %q; supported: %s", route.Model, route.Effort, strings.Join(supported, ", "))
	}
	return route, fmt.Errorf("model %q is not in your Codex catalog; run relay-agent models", route.Model)
}
