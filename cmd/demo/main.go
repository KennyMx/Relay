// Command demo verifies the running gateway using only mock routes.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type demo struct {
	base, admin string
	client      *http.Client
}

func main() {
	d := demo{base: os.Getenv("RELAY_URL"), admin: os.Getenv("RELAY_ADMIN_TOKEN"), client: &http.Client{Timeout: 15 * time.Second}}
	if d.base == "" {
		d.base = "http://localhost:8080"
	}
	if d.admin == "" {
		d.admin = "local-demo-admin-token-change-before-sharing"
	}
	if err := d.run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func (d demo) call(method, path, token string, body any, want int, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, d.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return fmt.Errorf("%s %s: wanted HTTP %d, got %d", method, path, want, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}
func (d demo) run() (runErr error) {
	if err := d.call("GET", "/health", "", nil, 200, nil); err != nil {
		return err
	}
	var key struct {
		ID  string `json:"id"`
		Raw string `json:"api_key"`
	}
	if err := d.call("POST", "/v1/keys", d.admin, map[string]any{"name": "portfolio-demo", "requests_per_minute": 1, "burst": 4}, 201, &key); err != nil {
		return err
	}
	defer func() {
		if err := d.call("DELETE", "/v1/keys/"+key.ID, d.admin, nil, 204, nil); err != nil {
			runErr = err
		}
		if err := d.call("GET", "/v1/requests", key.Raw, nil, 401, nil); err != nil {
			runErr = err
		}
	}()
	for _, route := range []string{"chat", "demo-fallback", "demo-500", "demo-timeout"} {
		body := map[string]any{"model": route, "messages": []map[string]string{{"role": "user", "content": "hello world"}}}
		var out struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			Cost     int64  `json:"cost_nano_usd"`
			Fallback int    `json:"fallback_count"`
			Usage    struct {
				Simulated bool `json:"simulated"`
				Total     int  `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := d.call("POST", "/v1/chat/completions", key.Raw, body, 200, &out); err != nil {
			return err
		}
		expected := 1
		if route == "chat" {
			expected = 0
		}
		if out.Fallback != expected || !out.Usage.Simulated || out.Usage.Total != 6 || out.Cost != 10000 || out.Provider != "mock" {
			return fmt.Errorf("unexpected %s result: %+v", route, out)
		}
		var record struct {
			Status   string            `json:"status"`
			Attempts []json.RawMessage `json:"attempts"`
		}
		if err := d.call("GET", "/v1/requests/"+out.ID, key.Raw, nil, 200, &record); err != nil {
			return err
		}
		if record.Status != "success" || len(record.Attempts) != expected+1 {
			return fmt.Errorf("missing ledger attempts for %s", route)
		}
		fmt.Printf("PASS %-14s request=%s attempts=%d tokens=%d simulated_cost=$%.8f\n", route, out.ID, len(record.Attempts), out.Usage.Total, float64(out.Cost)/1e9)
	}
	if err := d.call("POST", "/v1/chat/completions", key.Raw, map[string]any{"messages": []map[string]string{{"role": "user", "content": "quota test"}}}, 429, nil); err != nil {
		return err
	}
	fmt.Println("PASS per-key quota returns HTTP 429; demo key will now be revoked")
	return nil
}
