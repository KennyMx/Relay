// Command server serves product documentation on Vercel.
// The authenticated, persistent gateway is cmd/relay.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/KennyMx/Relay/internal/webui"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	server := http.Server{Addr: ":" + port, Handler: webui.PublicHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("product site stopped", "error", err)
		os.Exit(1)
	}
}
