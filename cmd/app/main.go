package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	httpserver "github.com/sploitzberg/go-hexagonal-architecture-template/internal/adapters/in/http"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/adapters/out/memory"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/app"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/config"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		return 1
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	store := memory.NewStore()
	svc := app.New(store)
	handler := httpserver.NewHandler(svc, log, cfg.MaxRequestBodyBytes)

	srv := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr)
		errCh <- srv.ListenAndServe()
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			return 1
		}
		return 0
	case s := <-sig:
		log.Info("shutdown", "signal", s.String())
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("shutdown", "err", err)
			return 1
		}
		err := <-errCh
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			return 1
		}
		log.Info("stopped")
		return 0
	}
}
