package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"stockroom/internal/ai"
	"stockroom/internal/httpapi"
	"stockroom/internal/store"
)

var version = "0.1.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		address := "http://127.0.0.1:8026/healthz"
		if len(os.Args) > 2 {
			address = os.Args[2]
		}
		if err := checkHealth(address); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := newLogger()
	databasePath := env("DATABASE_PATH", "./data/wubu.db")
	database, err := store.Open(databasePath)
	if err != nil {
		logger.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	configuredAnalyzer, err := ai.FromEnvironment()
	if err != nil {
		logger.Error("local AI configuration failed", "error", err)
		os.Exit(1)
	}
	var analyzer ai.Analyzer
	if configuredAnalyzer != nil {
		analyzer = configuredAnalyzer
		logger.Info("local AI configured", "model", analyzer.Model())
	}
	handler, err := httpapi.New(database, version, logger, analyzer)
	if err != nil {
		logger.Error("HTTP startup failed", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              env("LISTEN_ADDR", "127.0.0.1:8026"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	workerContext, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go runAutoConsumptionWorker(workerContext, database, logger)

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Wubu started", "address", server.Addr, "database", databasePath, "version", version)
		serverErrors <- server.ListenAndServe()
	}()

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", err)
			os.Exit(1)
		}
	case <-signalContext.Done():
		logger.Info("shutdown requested")
	}
	stopWorker()

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("Stockroom stopped")
}

func runAutoConsumptionWorker(ctx context.Context, database *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			count, err := database.ApplyDueAutoConsumption(checkContext)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("auto-consumption check failed", "error", err)
			} else if count > 0 {
				logger.Info("short-lived items auto-consumed", "count", count)
			}
		}
	}
}

func newLogger() *slog.Logger {
	options := &slog.HandlerOptions{Level: slog.LevelInfo}
	if strings.EqualFold(os.Getenv("LOG_FORMAT"), "json") {
		return slog.New(slog.NewJSONHandler(os.Stdout, options))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, options))
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func checkHealth(address string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	request, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("health request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health request returned HTTP %d", response.StatusCode)
	}
	return nil
}
