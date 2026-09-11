// Command verify exercises a running Relay stack using local provider routes.
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

type verification struct {
	base, admin string
	client      *http.Client
}

func main() {
	v := verification{
		base:   os.Getenv("RELAY_URL"),
		admin:  os.Getenv("RELAY_ADMIN_TOKEN"),
		client: &http.Client{Timeout: 15 * time.Second},
	}
	if v.base == "" {
		v.base = "http://localhost:8080"
	}
	if v.admin == "" {
		fmt.Fprintln(os.Stderr, "RELAY_ADMIN_TOKEN is required")
		os.Exit(1)
	}
	if err := v.run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (v verification) call(method, path, token string, body any, want int, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, v.base+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(req)
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

func (v verification) run() (runErr error) {
	if err := v.call("GET", "/health", "", nil, http.StatusOK, nil); err != nil {
		return err
	}
	var key struct {
		ID  string `json:"id"`
		Raw string `json:"api_key"`
	}
	if err := v.call("POST", "/v1/keys", v.admin, map[string]any{
		"name": "verification-key", "requests_per_minute": 1, "burst": 4,
	}, http.StatusCreated, &key); err != nil {
		return err
	}
	defer func() {
		if err := v.call("DELETE", "/v1/keys/"+key.ID, v.admin, nil, http.StatusNoContent, nil); err != nil {
			runErr = err
		}
		if err := v.call("GET", "/v1/requests", key.Raw, nil, http.StatusUnauthorized, nil); err != nil {
			runErr = err
		}
	}()

	for _, route := range []string{"chat", "fallback-rate-limit", "fallback-server-error", "fallback-timeout"} {
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
		if err := v.call("POST", "/v1/chat/completions", key.Raw, body, http.StatusOK, &out); err != nil {
			return err
		}
		expectedAttempts := 2
		if route == "chat" {
			expectedAttempts = 1
		}
		if out.Fallback != expectedAttempts-1 || !out.Usage.Simulated || out.Usage.Total != 6 || out.Cost != 10000 || out.Provider != "mock" {
			return fmt.Errorf("unexpected %s result: %+v", route, out)
		}
		var record struct {
			Status   string            `json:"status"`
			Attempts []json.RawMessage `json:"attempts"`
		}
		if err := v.call("GET", "/v1/requests/"+out.ID, key.Raw, nil, http.StatusOK, &record); err != nil {
			return err
		}
		if record.Status != "success" || len(record.Attempts) != expectedAttempts {
			return fmt.Errorf("missing ledger attempts for %s", route)
		}
		fmt.Printf("PASS %-22s request=%s attempts=%d tokens=%d simulated_cost=$%.8f\n", route, out.ID, len(record.Attempts), out.Usage.Total, float64(out.Cost)/1e9)
	}

	if err := v.call("POST", "/v1/chat/completions", key.Raw, map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "quota test"}},
	}, http.StatusTooManyRequests, nil); err != nil {
		return err
	}
	fmt.Println("PASS per-key quota returns HTTP 429; verification key will now be revoked")
	return nil
}
