package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCatalog() []codexModel {
	var models []codexModel
	for _, name := range []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "future-model"} {
		m := codexModel{Model: name, DefaultEffort: "medium"}
		for _, level := range []string{"low", "medium", "high", "xhigh"} {
			m.Efforts = append(m.Efforts, struct {
				Effort string `json:"reasoningEffort"`
			}{level})
		}
		models = append(models, m)
	}
	return models
}

func TestCodexRoutingAndEfforts(t *testing.T) {
	for _, tc := range []struct{ prompt, model, effort string }{
		{"Summarize these notes.", "gpt-6-luna", "low"},
		{"Explain this function.", "gpt-6-sol", "medium"},
		{"Find a race condition.", "gpt-6-astra", "high"},
	} {
		d, err := selectCodexRoute(nativeTaskOptions{Mode: "auto", Prompt: tc.prompt}, testCatalog())
		if err != nil || d.Model != tc.model || d.Effort != tc.effort {
			t.Fatalf("route: %+v %v", d, err)
		}
	}
	// Every catalog entry, including unknown future IDs, can be selected.
	for _, model := range testCatalog() {
		for _, level := range []string{"low", "medium", "high", "xhigh", "light", "extra-high"} {
			d, err := selectCodexRoute(nativeTaskOptions{Mode: "auto", Model: model.Model, Effort: level}, testCatalog())
			if err != nil || d.Model != model.Model {
				t.Fatalf("explicit model: %+v %v", d, err)
			}
		}
	}
	for _, opts := range []nativeTaskOptions{
		{Mode: "auto", Model: "missing"},
		{Mode: "auto", Model: "gpt-5.5", Effort: "ultra"},
		{Mode: "wrong"},
	} {
		if _, err := selectCodexRoute(opts, testCatalog()); err == nil {
			t.Fatalf("accepted invalid options: %+v", opts)
		}
	}
}

func TestConfiguredRoutesSupportAnyCatalogModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routing.json")
	content := `{"simple":{"model":"gpt-5.6-luna","effort":"low"},"standard":{"model":"gpt-5.6-terra","effort":"high"},"complex":{"model":"gpt-5.5","effort":"xhigh"}}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ prompt, model, effort string }{
		{"Summarize notes", "gpt-5.6-luna", "low"},
		{"Explain code", "gpt-5.6-terra", "high"},
		{"Review architecture", "gpt-5.5", "xhigh"},
	} {
		d, err := selectCodexRoute(nativeTaskOptions{Mode: "auto", Prompt: tc.prompt, RoutingPath: path}, testCatalog())
		if err != nil || d.Model != tc.model || d.Effort != tc.effort {
			t.Fatalf("config: %+v %v", d, err)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Replace(content, "gpt-5.5", "missing", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := selectCodexRoute(nativeTaskOptions{Mode: "cheap", RoutingPath: path}, testCatalog()); err == nil {
		t.Fatal("invalid configured model silently replaced")
	}
}

func TestCatalogPaginationAndErrors(t *testing.T) {
	var input bytes.Buffer
	stream := `{"id":1,"result":{}}
{"method":"notice"}
{"id":2,"result":{"data":[{"model":"first"}],"nextCursor":"page2"}}
{"id":3,"result":{"data":[{"model":"second"}],"nextCursor":null}}
`
	models, err := readCodexCatalog(&input, strings.NewReader(stream))
	if err != nil || len(models) != 2 {
		t.Fatalf("catalog: %+v %v", models, err)
	}
	decoder := json.NewDecoder(&input)
	for i := 0; i < 4; i++ {
		var req map[string]any
		if err := decoder.Decode(&req); err != nil {
			t.Fatal(err)
		}
		if i == 3 && req["params"].(map[string]any)["cursor"] != "page2" {
			t.Fatal("cursor not sent")
		}
	}
	for _, bad := range []string{
		`{"id":1,"error":{"message":"not authenticated"}}`,
		`{"id":1,"result":{}}` + "\n" + `{"id":2,"result":{"data":[]}}`,
		`{"id":1,"result":{}}` + "\n" + `{"id":2,"result":{"data":[{"model":"m"}],"nextCursor":"same"}}` + "\n" + `{"id":3,"result":{"data":[{"model":"m"}],"nextCursor":"same"}}`,
	} {
		if _, err := readCodexCatalog(&bytes.Buffer{}, strings.NewReader(bad)); err == nil {
			t.Fatal("accepted invalid catalog")
		}
	}
}
