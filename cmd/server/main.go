// Command server runs the isolated public workspace on Vercel.
// The authenticated, persistent gateway is cmd/relay.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/KennyMx/Relay/internal/trial"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	server := http.Server{Addr: ":" + port, Handler: trial.PublicHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("public workspace stopped", "error", err)
		os.Exit(1)
	}
}
