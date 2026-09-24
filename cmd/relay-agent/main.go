// Command relay-agent delegates small, explicit tasks to a self-hosted Relay route.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const maxPromptBytes = 8 << 10

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type completion struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Route    string `json:"route"`
	Choices  []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		Input     int  `json:"input_tokens"`
		Output    int  `json:"output_tokens"`
		Simulated bool `json:"simulated"`
	} `json:"usage"`
	CostNanoUSD int64 `json:"cost_nano_usd"`
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "models" || os.Args[1] == "run" || os.Args[1] == "runs" || os.Args[1] == "serve") {
		if err := runNativeCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "relay-agent:", err)
			os.Exit(1)
		}
		return
	}
	client := &http.Client{
		Timeout:       40 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if len(os.Args) == 2 && os.Args[1] == "mcp" {
		if err := serveMCP(context.Background(), client); err != nil {
			fmt.Fprintln(os.Stderr, "relay-agent:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:], os.Stdin, os.Stdout, client); err != nil {
		fmt.Fprintln(os.Stderr, "relay-agent:", err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer, client httpDoer) error {
	if len(args) == 0 || (args[0] != "ask" && args[0] != "check" && args[0] != "report") {
		return errors.New("usage: relay-agent ask [--route chat] [--max-tokens 256] [--json] < prompt.txt | relay-agent check | relay-agent report --reference-input USD_PER_M --reference-output USD_PER_M | relay-agent mcp")
	}
	base, err := gatewayURL(os.Getenv("RELAY_GATEWAY_URL"))
	if err != nil {
		return err
	}
	key := os.Getenv("RELAY_KEY")
	if key == "" {
		return errors.New("RELAY_KEY is required; create a Relay key in the local console")
	}
	if args[0] == "check" {
		if len(args) != 1 {
			return errors.New("usage: relay-agent check")
		}
		return check(base, key, output, client)
	}
	if args[0] == "report" {
		return report(args[1:], base, key, output, client)
	}
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	route := fs.String("route", "chat", "configured Relay route")
	maxTokens := fs.Int("max-tokens", 256, "maximum output tokens")
	jsonOutput := fs.Bool("json", false, "return structured JSON")
	if err := fs.Parse(args[1:]); err != nil || len(fs.Args()) != 0 {
		return errors.New("usage: relay-agent ask [--route chat] [--max-tokens 256] [--json] < prompt.txt")
	}
	data, err := io.ReadAll(io.LimitReader(input, maxPromptBytes+1))
	if err != nil {
		return fmt.Errorf("read task: %w", err)
	}
	prompt := strings.TrimSpace(string(data))
	if len(data) > maxPromptBytes {
		return fmt.Errorf("task must be nonempty and at most %d bytes", maxPromptBytes)
	}
	result, err := delegate(context.Background(), base, key, *route, *maxTokens, prompt, client)
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(output).Encode(result)
	}
	_, err = fmt.Fprintf(output, "%s\n\n[Relay: %s/%s, route=%s, input=%d, output=%d, estimated=$%.8f, simulated=%t, request=%s]\n",
		result.Choices[0].Message.Content, result.Provider, result.Model, result.Route,
		result.Usage.Input, result.Usage.Output, float64(result.CostNanoUSD)/1e9,
		result.Usage.Simulated, result.ID)
	return err
}

func delegate(ctx context.Context, base, key, route string, maxTokens int, prompt string, client httpDoer) (completion, error) {
	var result completion
	if maxTokens < 1 || maxTokens > 512 || route == "" || len(route) > 64 || strings.ContainsAny(route, " \t\r\n") {
		return result, errors.New("route must be a short alias and max-tokens must be between 1 and 512")
	}
	if strings.TrimSpace(prompt) == "" || len(prompt) > maxPromptBytes {
		return result, fmt.Errorf("task must be nonempty and at most %d bytes", maxPromptBytes)
	}
	requestBody, _ := json.Marshal(map[string]any{
		"model": route, "messages": []map[string]string{{"role": "user", "content": prompt}}, "max_tokens": maxTokens,
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(requestBody))
	if err != nil {
		return result, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return result, fmt.Errorf("gateway unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, gatewayError(response)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return result, fmt.Errorf("invalid gateway response: %w", err)
	}
	if len(result.Choices) != 1 || result.Choices[0].Message.Content == "" || result.ID == "" || result.Provider == "" {
		return result, errors.New("invalid gateway completion")
	}
	return result, nil
}

func check(base, key string, output io.Writer, client httpDoer) error {
	for _, path := range []string{"/health", "/v1/routes"} {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		if path == "/v1/routes" {
			request.Header.Set("Authorization", "Bearer "+key)
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("gateway unavailable: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			err = gatewayError(response)
			response.Body.Close()
			return err
		}
		if path == "/v1/routes" {
			var routes struct {
				Default string `json:"default_route"`
				Routes  []struct {
					Name string `json:"name"`
				} `json:"routes"`
			}
			err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&routes)
			response.Body.Close()
			if err != nil {
				return fmt.Errorf("invalid routes response: %w", err)
			}
			if routes.Default == "" {
				return errors.New("gateway returned no default route")
			}
			names := make([]string, 0, len(routes.Routes))
			for _, route := range routes.Routes {
				names = append(names, route.Name)
			}
			_, err = fmt.Fprintf(output, "Relay ready; default route: %s; routes: %s\n", routes.Default, strings.Join(names, ", "))
			return err
		}
		response.Body.Close()
	}
	return nil
}

func gatewayError(response *http.Response) error {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 4<<10)).Decode(&body)
	if body.Error.Code != "" {
		return fmt.Errorf("gateway HTTP %d: %s", response.StatusCode, body.Error.Code)
	}
	return fmt.Errorf("gateway HTTP %d", response.StatusCode)
}

func gatewayURL(raw string) (string, error) {
	if raw == "" {
		raw = "http://127.0.0.1:8080"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("RELAY_GATEWAY_URL must be a gateway origin without credentials or a path")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !(strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())) {
			return "", errors.New("RELAY_GATEWAY_URL must use HTTPS except on loopback")
		}
	}
	return strings.TrimRight(u.String(), "/"), nil
}
