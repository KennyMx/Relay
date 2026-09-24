package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func readNativeRuns(path string) ([]nativeRun, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 8<<20))
	var runs []nativeRun
	for {
		var r nativeRun
		if err := decoder.Decode(&r); errors.Is(err, io.EOF) {
			return runs, nil
		} else if err != nil {
			return nil, err
		}
		if len(runs) == 1000 {
			runs = runs[1:]
		}
		runs = append(runs, r)
	}
}

func nativeRunsHandler(path string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		ip := net.ParseIP(host)
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "untrusted host", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		runs, err := readNativeRuns(path)
		if err != nil {
			http.Error(w, "Run log unavailable", http.StatusServiceUnavailable)
			return
		}
		view := struct {
			Runs   []nativeRun
			Count  int
			Codex  int
			Claude int
			Input  int64
			Cached int64
			Output int64
		}{Runs: runs, Count: len(runs)}
		for _, run := range runs {
			if run.Host == "codex" {
				view.Codex++
			} else if run.Host == "claude" {
				view.Claude++
			}
			view.Input += run.InputTokens
			view.Cached += run.CachedInputTokens
			view.Output += run.OutputTokens
		}
		for i, j := 0, len(view.Runs)-1; i < j; i, j = i+1, j-1 {
			view.Runs[i], view.Runs[j] = view.Runs[j], view.Runs[i]
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := runsPage.Execute(w, view); err != nil {
			return
		}
	})
}

func serveNativeRuns(args []string, output io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8787", "loopback listen address")
	logPath := fs.String("log", ".relay/runs.jsonl", "metadata-only run log")
	if err := fs.Parse(args); err != nil || len(fs.Args()) != 0 {
		return errors.New("usage: relay-agent serve [--addr 127.0.0.1:8787] [--log PATH]")
	}
	host, _, err := net.SplitHostPort(*addr)
	if err != nil {
		return errors.New("addr must be a host:port loopback address")
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("run UI must bind to loopback")
	}
	server := &http.Server{Addr: *addr, Handler: nativeRunsHandler(*logPath), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	if _, err := fmt.Fprintf(output, "Relay runs: http://%s\n", *addr); err != nil {
		return err
	}
	return server.ListenAndServe()
}

var runsPage = template.Must(template.New("runs").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Relay · Native runs</title><style>
:root{color-scheme:dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0a0e17;color:#eaf0f6}*{box-sizing:border-box}body{margin:0;background:radial-gradient(circle at 70% -20%,#173b4b 0,transparent 45%),#0a0e17;min-height:100vh}main{max-width:1380px;margin:auto;padding:44px 28px 80px}header{display:flex;justify-content:space-between;align-items:center;margin-bottom:76px}.brand{font-size:23px;font-weight:800;letter-spacing:-.05em}.brand span{color:#4be0b6}.tag{border:1px solid #335667;color:#86ddc8;padding:8px 12px;border-radius:99px;font:12px ui-monospace,monospace}h1{font-size:clamp(42px,6vw,72px);letter-spacing:-.065em;line-height:1.02;margin:0 0 18px;max-width:720px}.lead{color:#a8b7c8;font-size:17px;line-height:1.6;max-width:720px;margin:0 0 44px}.eyebrow{color:#4be0b6;font:12px ui-monospace,monospace;letter-spacing:.16em;text-transform:uppercase;margin-bottom:18px}.cards{display:grid;grid-template-columns:repeat(4,1fr);gap:14px;margin-bottom:36px}.card{background:#121c2a;border:1px solid #233449;border-radius:16px;padding:24px}.card small{color:#90a5b9;display:block;margin-bottom:12px}.card strong{font-size:34px;letter-spacing:-.05em}.card em{display:block;color:#7f94a7;font-size:12px;font-style:normal;margin-top:8px}.panel{background:#111a27;border:1px solid #233449;border-radius:18px;overflow:hidden}.panelhead{padding:22px 24px;display:flex;justify-content:space-between;align-items:center;border-bottom:1px solid #233449}.panelhead h2{margin:0;font-size:20px;letter-spacing:-.03em}.panelhead span{color:#86a0b6;font-size:13px}.scroll{overflow:auto}table{border-collapse:collapse;width:100%;min-width:850px}th{text-align:left;color:#8196a9;font:11px ui-monospace,monospace;letter-spacing:.1em;text-transform:uppercase;padding:15px 24px}td{border-top:1px solid #1c2a3a;padding:16px 24px;font-size:14px;white-space:nowrap}td.mono{font-family:ui-monospace,monospace;color:#abc1d1}.pill{display:inline-block;background:#193b3d;color:#7fe0c5;border-radius:99px;padding:5px 9px;font-size:12px}.pill.fail{background:#3c222b;color:#f3a6b8}.empty{padding:64px 24px;text-align:center;color:#8fa5b8}footer{margin-top:24px;color:#7f93a6;font-size:13px;line-height:1.6}footer code{color:#aadcca}@media(max-width:740px){header{margin-bottom:58px}.cards{grid-template-columns:repeat(2,1fr)}main{padding:28px 18px 56px}.card{padding:18px}.card strong{font-size:28px}}
</style></head><body><main><header><div class="brand">relay<span>.</span></div><span class="tag">CODEX RUN HISTORY</span></header><p class="eyebrow">Codex model routing</p><h1>Real runs.<br>Visible usage.</h1><p class="lead">These are Codex CLI sessions launched through Relay. The log stores run metadata and reported usage, not your prompts or answers.</p><section class="cards" aria-label="Usage summary"><div class="card"><small>Runs recorded</small><strong>{{.Count}}</strong><em>{{.Codex}} Codex{{if .Claude}} · {{.Claude}} experimental Claude{{end}}</em></div><div class="card"><small>Input tokens</small><strong>{{.Input}}</strong><em>Reported by native hosts</em></div><div class="card"><small>Cached input</small><strong>{{.Cached}}</strong><em>Included in input total</em></div><div class="card"><small>Output tokens</small><strong>{{.Output}}</strong><em>Reported by native hosts</em></div></section><section class="panel"><div class="panelhead"><h2>Recent sessions</h2><span>Refresh to update</span></div>{{if .Runs}}<div class="scroll"><table><thead><tr><th>When (UTC)</th><th>Host</th><th>Route</th><th>Model</th><th>Effort</th><th>Input</th><th>Cached</th><th>Output</th><th>Duration</th><th>Status</th></tr></thead><tbody>{{range .Runs}}<tr><td class="mono">{{.StartedAt}}</td><td>{{.Host}}</td><td>{{.Tier}}</td><td class="mono">{{.RequestedModel}}</td><td class="mono">{{if .ReasoningEffort}}{{.ReasoningEffort}}{{else}}host default{{end}}</td><td class="mono">{{.InputTokens}}</td><td class="mono">{{.CachedInputTokens}}</td><td class="mono">{{.OutputTokens}}</td><td class="mono">{{.DurationMS}}ms</td><td>{{if .Success}}<span class="pill">Completed</span>{{else}}<span class="pill fail">Failed</span>{{end}}</td></tr>{{end}}</tbody></table></div>{{else}}<div class="empty">No native runs yet. Use <code>relay-agent run --host codex</code> to begin.</div>{{end}}</section><footer>Token counts are observations, not a claim of saved tokens or lower subscription charges. Codex does not report per-run subscription billing here.</footer></main></body></html>`))
