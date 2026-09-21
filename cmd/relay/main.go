package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/KennyMx/Relay/internal/api"
	"github.com/KennyMx/Relay/internal/config"
	"github.com/KennyMx/Relay/internal/ratelimit"
	"github.com/KennyMx/Relay/internal/store"
	"github.com/KennyMx/Relay/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("relay stopped", "error", err)
		os.Exit(1)
	}
}
func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func run() error {
	if len(os.Args) >= 2 && os.Args[1] == "init" {
		path := ".env"
		if len(os.Args) == 3 {
			path = os.Args[2]
		} else if len(os.Args) != 2 {
			return fmt.Errorf("usage: relay init [path]")
		}
		if err := config.InitEnv(path); err != nil {
			return err
		}
		slog.Info("Created credentials file with owner-only permissions; keep it private", "path", path)
		return nil
	}
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get("http://127.0.0.1:8080/health")
		if err != nil {
			return fmt.Errorf("healthcheck failed")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("healthcheck status %d", resp.StatusCode)
		}
		return nil
	}
	admin := os.Getenv("RELAY_ADMIN_TOKEN")
	if err := config.ValidateAdminToken(admin); err != nil {
		return err
	}
	cfg, err := config.Load(env("RELAY_CONFIG", "config/relay.json"))
	if err != nil {
		return err
	}
	routing, err := cfg.BuildRouter()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("invalid database configuration")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return fmt.Errorf("PostgreSQL unavailable")
	}
	if err = migrations.Apply(ctx, pool); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}
	options, err := redis.ParseURL(env("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		return fmt.Errorf("invalid Redis configuration")
	}
	options.ContextTimeoutEnabled = true
	rc := redis.NewClient(options)
	defer rc.Close()
	if err = rc.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis unavailable")
	}
	app := api.Server{AllowedHosts: strings.Split(env("RELAY_ALLOWED_HOSTS", "localhost,127.0.0.1,::1"), ","), Store: &store.Store{Pool: pool}, Limiter: &ratelimit.Bucket{Client: rc}, Router: routing, Pricing: cfg.Pricing, AdminToken: admin, Timeout: time.Duration(cfg.RequestTimeoutMS) * time.Millisecond, Health: func(ctx context.Context) error {
		if err := pool.Ping(ctx); err != nil {
			return err
		}
		return rc.Ping(ctx).Err()
	}}
	server := http.Server{Addr: env("RELAY_ADDR", ":"+env("PORT", "8080")), Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: time.Duration(cfg.RequestTimeoutMS)*time.Millisecond + 5*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() {
		slog.Info("Relay listening", "addr", server.Addr, "default_route", cfg.DefaultRoute)
		done <- server.ListenAndServe()
	}()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-stopCtx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.RequestTimeoutMS)*time.Millisecond+5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
	}
	return nil
}
