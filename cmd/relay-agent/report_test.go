package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestReportSeparatesRealAndSimulated(t *testing.T) {
	t.Setenv("RELAY_KEY", "fixture-key")
	t.Setenv("RELAY_GATEWAY_URL", "http://127.0.0.1:8080")
	var output bytes.Buffer
	err := run([]string{"report", "--reference-input", "2", "--reference-output", "4"}, strings.NewReader(""), &output, fakeDoer(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" || r.URL.Path != "/v1/requests" {
			t.Error("incorrect ledger request")
		}
		return reply(200, `{"data":[{"status":"success","usage":{"input_tokens":100,"output_tokens":10,"simulated":false},"cost_nano_usd":100000,"routing":{"classification":{"cost_nano_usd":1000}}},{"status":"success","usage":{"input_tokens":50,"output_tokens":5,"simulated":true},"cost_nano_usd":10000},{"status":"error","usage":{"input_tokens":1000,"output_tokens":1000,"simulated":false},"cost_nano_usd":9999999}]}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"REAL: 1 requests, 100 input + 10 output", "Relay estimate $0.00010100", "reference estimate $0.00024000", "SIMULATED: 1 requests, 50 input + 5 output", "Simulated figures are not real savings"} {
		if !strings.Contains(output.String(), s) {
			t.Errorf("missing %q in %s", s, output.String())
		}
	}
}

func TestReportRejectsInvalidPrice(t *testing.T) {
	for _, raw := range []string{"-1", "NaN", "Inf", "foo"} {
		if _, err := referenceRate(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
