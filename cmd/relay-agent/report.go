package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
)

// A reference comparison prices the same observed tokens at user-supplied rates.
// It cannot measure the host agent's billing or answer quality.
type ledgerRecord struct {
	Status string `json:"status"`
	Usage  struct {
		Input     int64 `json:"input_tokens"`
		Output    int64 `json:"output_tokens"`
		Simulated bool  `json:"simulated"`
	} `json:"usage"`
	CostNanoUSD int64 `json:"cost_nano_usd"`
	Routing     *struct {
		Classification *struct {
			CostNanoUSD int64 `json:"cost_nano_usd"`
		} `json:"classification"`
	} `json:"routing"`
}

type comparison struct {
	Requests                 int   `json:"requests"`
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	RelayEstimateNanoUSD     int64 `json:"relay_estimate_nano_usd"`
	ReferenceEstimateNanoUSD int64 `json:"reference_estimate_nano_usd"`
}

func referenceRate(raw string) (int64, error) {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1000000 {
		return 0, errors.New("reference prices must be nonnegative USD per million tokens")
	}
	return int64(math.Round(f * 1000)), nil
}

func accumulate(out *comparison, r ledgerRecord, inRate, outRate int64) error {
	if r.Usage.Input < 0 || r.Usage.Output < 0 || r.CostNanoUSD < 0 || r.Usage.Input > math.MaxInt64/inRateIfZero(inRate) || r.Usage.Output > math.MaxInt64/inRateIfZero(outRate) {
		return errors.New("invalid ledger usage or price overflow")
	}
	classification := int64(0)
	if r.Routing != nil && r.Routing.Classification != nil {
		classification = r.Routing.Classification.CostNanoUSD
	}
	if classification < 0 {
		return errors.New("invalid classification cost")
	}
	if r.CostNanoUSD > math.MaxInt64-classification {
		return errors.New("report cost overflow")
	}
	actual := r.CostNanoUSD + classification
	inputBaseline, outputBaseline := r.Usage.Input*inRate, r.Usage.Output*outRate
	if inputBaseline > math.MaxInt64-outputBaseline {
		return errors.New("report cost overflow")
	}
	baseline := inputBaseline + outputBaseline
	if out.RelayEstimateNanoUSD > math.MaxInt64-actual || out.ReferenceEstimateNanoUSD > math.MaxInt64-baseline || out.InputTokens > math.MaxInt64-r.Usage.Input || out.OutputTokens > math.MaxInt64-r.Usage.Output {
		return errors.New("report cost overflow")
	}
	out.Requests++
	out.InputTokens += r.Usage.Input
	out.OutputTokens += r.Usage.Output
	out.RelayEstimateNanoUSD += actual
	out.ReferenceEstimateNanoUSD += baseline
	return nil
}

func inRateIfZero(rate int64) int64 {
	if rate == 0 {
		return 1
	}
	return rate
}

func report(args []string, base, key string, output io.Writer, client httpDoer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	inputPrice := fs.String("reference-input", "", "reference USD per million input tokens")
	outputPrice := fs.String("reference-output", "", "reference USD per million output tokens")
	limit := fs.Int("limit", 100, "maximum recent ledger entries to inspect")
	jsonOutput := fs.Bool("json", false, "structured JSON")
	if err := fs.Parse(args); err != nil || len(fs.Args()) != 0 || *inputPrice == "" || *outputPrice == "" || *limit < 1 || *limit > 1000 {
		return errors.New("usage: relay-agent report --reference-input USD_PER_M --reference-output USD_PER_M [--limit 100] [--json]")
	}
	inRate, err := referenceRate(*inputPrice)
	if err != nil {
		return err
	}
	outRate, err := referenceRate(*outputPrice)
	if err != nil {
		return err
	}
	var real, simulated comparison
	inspected := 0
	for inspected < *limit {
		pageSize := min(100, *limit-inspected)
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fmt.Sprintf("%s/v1/requests?limit=%d&offset=%d", base, pageSize, inspected), nil)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("gateway unavailable: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			err = gatewayError(response)
			response.Body.Close()
			return err
		}
		var page struct {
			Data []ledgerRecord `json:"data"`
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page)
		response.Body.Close()
		if err != nil {
			return fmt.Errorf("invalid ledger response: %w", err)
		}
		for _, r := range page.Data {
			if r.Status != "success" {
				continue
			}
			if r.Usage.Simulated {
				err = accumulate(&simulated, r, inRate, outRate)
			} else {
				err = accumulate(&real, r, inRate, outRate)
			}
			if err != nil {
				return err
			}
		}
		inspected += len(page.Data)
		if len(page.Data) < pageSize {
			break
		}
	}
	if *jsonOutput {
		return json.NewEncoder(output).Encode(map[string]any{"inspected": inspected, "reference_input_usd_per_m": *inputPrice, "reference_output_usd_per_m": *outputPrice, "real": real, "simulated": simulated})
	}
	_, err = fmt.Fprintf(output, "Relay cost comparison (%d recent key-scoped requests; successful only)\nReference: input $%s/M, output $%s/M; same observed tokens\nREAL: %d requests, %d input + %d output tokens, Relay estimate $%.8f, reference estimate $%.8f, modeled difference $%.8f\nSIMULATED: %d requests, %d input + %d output tokens, Relay estimate $%.8f, reference estimate $%.8f, modeled difference $%.8f\nEstimates include recorded classifier cost. This does not measure host-agent billing, total session cost, or answer quality. Simulated figures are not real savings.\n",
		inspected, *inputPrice, *outputPrice, real.Requests, real.InputTokens, real.OutputTokens, float64(real.RelayEstimateNanoUSD)/1e9, float64(real.ReferenceEstimateNanoUSD)/1e9, float64(real.ReferenceEstimateNanoUSD-real.RelayEstimateNanoUSD)/1e9,
		simulated.Requests, simulated.InputTokens, simulated.OutputTokens, float64(simulated.RelayEstimateNanoUSD)/1e9, float64(simulated.ReferenceEstimateNanoUSD)/1e9, float64(simulated.ReferenceEstimateNanoUSD-simulated.RelayEstimateNanoUSD)/1e9)
	return err
}
