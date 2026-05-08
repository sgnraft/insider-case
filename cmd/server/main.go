package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/sgnraft/insider-case/internal/api"
	"github.com/sgnraft/insider-case/internal/config"
	"github.com/sgnraft/insider-case/internal/delivery"
	"github.com/sgnraft/insider-case/internal/metrics"
	"github.com/sgnraft/insider-case/internal/queue"
	"github.com/sgnraft/insider-case/internal/repository"
	"github.com/sgnraft/insider-case/internal/scheduler"
)

func main() {
	cfg := config.Load()
	logger := setupLogger()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// PostgreSQL
	db, err := setupDatabase(cfg)
	if err != nil {
		logger.Error("database connection failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("database connected", "dsn_prefix", cfg.Database.DSN[:min(len(cfg.Database.DSN), 30)])

	// Redis
	rdb, err := setupRedis(ctx, cfg)
	if err != nil {
		logger.Error("redis connection failed", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()
	logger.Info("redis connected", "addr", cfg.Redis.Addr)

	// Wire up dependencies
	repo := repository.New(db, logger)
	q := queue.New(rdb, logger)
	m := metrics.New()
	provider := delivery.New(cfg.Provider.WebhookURL, cfg.Provider.Timeout, logger)

	// Worker pool
	w := queue.NewWorker(repo, q, provider, m, logger,
		cfg.Worker.Concurrency,
		float64(cfg.Worker.RateLimitPerSec),
	)
	go w.Start(ctx, cfg.Worker.PollInterval)

	// Scheduler (scheduled notifications + retries)
	if cfg.Worker.SchedulerEnabled {
		sched := scheduler.New(repo, q, logger)
		go sched.Start(ctx)
	}

	// Refresh queue-depth gauges every 5 s
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				for priority, depth := range q.Depths(ctx) {
					m.SetQueueDepth(priority, depth)
				}
			}
		}
	}()

	// HTTP server
	router := api.NewRouter(repo, q, m, logger, cfg.Worker.MaxRetries)
	server := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		logger.Info("http server starting", "addr", cfg.Server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down gracefully…")
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	if err := server.Shutdown(shutCtx); err != nil {
		logger.Error("server shutdown error", "err", err)
	}
	logger.Info("server stopped")
}

func setupLogger() *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if os.Getenv("APP_ENV") == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func setupDatabase(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(cfg.Database.MaxConnections)
	db.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return db, nil
}

func setupRedis(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return rdb, nil
}
