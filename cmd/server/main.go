package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/endru/kiw-test/internal/config"
	"github.com/endru/kiw-test/internal/db"
	"github.com/endru/kiw-test/internal/webhook"
	"github.com/endru/kiw-test/internal/whatsapp"
)

func main() {
	// Structured JSON logging to stdout.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	slog.Info("configuration loaded",
		"port", cfg.Port,
		"api_version", cfg.APIVersion,
		"db_path", cfg.DBPath,
	)

	// Initialize SQLite Database.
	dbStore, err := db.NewSQLStore(cfg.DBPath)
	if err != nil {
		slog.Error("failed to initialize SQLite database", "error", err)
		os.Exit(1)
	}
	defer dbStore.Close()
	slog.Info("SQLite database initialized successfully", "path", cfg.DBPath)

	// Create WhatsApp client.
	waClient := whatsapp.NewClient(
		cfg.AccessToken,
		cfg.PhoneNumberID,
		cfg.GraphAPIBaseURL,
		cfg.APIVersion,
	)

	// Create webhook handler.
	whHandler := webhook.NewHandler(cfg.VerifyToken, waClient, dbStore)

	// Set up routes.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /webhook", whHandler.HandleVerification)
	mux.HandleFunc("POST /webhook", whHandler.HandleEvent)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Wrap with logging middleware.
	handler := loggingMiddleware(mux)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine.
	go func() {
		slog.Info("server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutting down server", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}

// loggingMiddleware logs each HTTP request with method, path, status, and duration.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
