// charts renders reproducible, dependency-free SVGs from recorded evaluations.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"strings"
)

type record struct {
	ID       string  `json:"id"`
	Reason   string  `json:"reason"`
	Wall     float64 `json:"wall_ms"`
	Cost     int64   `json:"simulated_completion_cost_nano_usd"`
	Baseline int64   `json:"simulated_all_reasoning_cost_nano_usd"`
}
type report struct {
	Timestamp string   `json:"timestamp"`
	Records   []record `json:"records"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func text(b *strings.Builder, x, y int, size int, color, value string) {
	fmt.Fprintf(b, `<text x="%d" y="%d" font-size="%d" fill="%s">%s</text>`, x, y, size, color, html.EscapeString(value))
}
func base(title, subtitle string, height int) *strings.Builder {
	b := &strings.Builder{}
	fmt.Fprintf(b, `<svg xmlns="http://www.w3.org/2000/svg" width="960" height="%d" viewBox="0 0 960 %d" role="img" aria-label="%s"><rect width="960" height="%d" rx="16" fill="#0e1726"/><g font-family="system-ui, sans-serif">`, height, height, html.EscapeString(title), height)
	text(b, 32, 45, 23, "#f1f5f9", title)
	text(b, 32, 73, 13, "#a7b6cc", subtitle)
	return b
}
func run() error {
	data, err := os.ReadFile("docs/benchmarks/jev.json")
	if err != nil {
		return err
	}
	var r report
	if err = json.Unmarshal(data, &r); err != nil {
		return err
	}
	if len(r.Records) == 0 {
		return fmt.Errorf("empty evaluation")
	}
	h := 150 + len(r.Records)*34
	b := base("Jev classification · measured latency", "Sequential requests, including network/TLS; completion and storage excluded. "+r.Timestamp, h)
	maxMS := 0.0
	var cost, baseline int64
	for _, v := range r.Records {
		if v.Wall > maxMS {
			maxMS = v.Wall
		}
		cost += v.Cost
		baseline += v.Baseline
	}
	if maxMS == 0 {
		maxMS = 1
	}
	for i, v := range r.Records {
		y := 102 + i*34
		color := "#55cbb6"
		if v.Reason != "classified" {
			color = "#f1ba65"
		}
		text(b, 32, y+17, 13, "#c8d5e6", v.ID)
		fmt.Fprintf(b, `<rect x="166" y="%d" width="%.2f" height="23" rx="4" fill="%s"/>`, y, v.Wall/maxMS*620, color)
		text(b, 800, y+17, 13, "#f1f5f9", fmt.Sprintf("%.1f ms", v.Wall))
	}
	text(b, 32, h-25, 12, "#a7b6cc", "12 synthetic cases · amber = low-confidence default route · small smoke sample, not a throughput benchmark")
	b.WriteString("</g></svg>\n")
	if err = os.WriteFile("docs/benchmarks/latency.svg", []byte(b.String()), 0600); err != nil {
		return err
	}
	b = base("Model routing · illustrative completion cost", "Same mock token counts at configured rates; classifier charges excluded. No real-model savings claim.", 300)
	if baseline == 0 {
		return fmt.Errorf("zero baseline cost")
	}
	for i, v := range []struct {
		label string
		cost  int64
		color string
	}{{"All reasoning", baseline, "#7b8fae"}, {"Jev-selected routes", cost, "#55cbb6"}} {
		y := 112 + i*62
		text(b, 32, y+20, 14, "#c8d5e6", v.label)
		fmt.Fprintf(b, `<rect x="200" y="%d" width="%.2f" height="30" rx="4" fill="%s"/>`, y, float64(v.cost)/float64(baseline)*520, v.color)
		text(b, 742, y+21, 14, "#f1f5f9", fmt.Sprintf("$%.7f", float64(v.cost)/1e9))
	}
	text(b, 32, 263, 13, "#a7b6cc", fmt.Sprintf("%.1f%% lower simulated completion cost across %d requests; answer quality was not evaluated.", 100*(1-float64(cost)/float64(baseline)), len(r.Records)))
	b.WriteString("</g></svg>\n")
	return os.WriteFile("docs/benchmarks/cost.svg", []byte(b.String()), 0600)
}
