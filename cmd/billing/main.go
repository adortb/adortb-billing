package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/adortb/adortb-billing/internal/advertiser_billing"
	"github.com/adortb/adortb-billing/internal/api"
	"github.com/adortb/adortb-billing/internal/consumer"
	"github.com/adortb/adortb-billing/internal/metrics"
	"github.com/adortb/adortb-billing/internal/platform"
	"github.com/adortb/adortb-billing/internal/publisher_billing"
	"github.com/adortb/adortb-billing/internal/repo"
	"github.com/adortb/adortb-billing/internal/tracing"
	"github.com/prometheus/client_golang/prometheus"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// ── OpenTelemetry tracing ──────────────────────────────
	otlpEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://jaeger:4318")
	tracingShutdown, err := tracing.Init(context.Background(), "adortb-billing", otlpEndpoint)
	if err != nil {
		slog.Warn("tracing init failed", "err", err)
	} else {
		defer func() { _ = tracingShutdown(context.Background()) }()
		slog.Info("tracing initialized", "otlp_endpoint", otlpEndpoint)
	}

	// ── 数据库 ──────────────────────────────────────────────
	db, err := repo.NewDB(repo.Config{
		Host:     getenv("PG_HOST", "localhost"),
		Port:     5432,
		User:     getenv("PG_USER", "adortb"),
		Password: getenv("PG_PASSWORD", "adortb_dev"),
		DBName:   getenv("PG_DBNAME", "adortb"),
	})
	if err != nil {
		slog.Error("connect db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── Redis ────────────────────────────────────────────────
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     getenv("REDIS_ADDR", "localhost:6379"),
		Password: getenv("REDIS_PASSWORD", ""),
		DB:       0,
	})
	defer rdb.Close()

	// ── Prometheus ───────────────────────────────────────────
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	m := metrics.New(reg)

	// ── 仓储层 ───────────────────────────────────────────────
	advRepo := repo.NewAdvertiserRepo(db)
	pubRepo := repo.NewPublisherRepo(db)
	platRepo := repo.NewPlatformRepo(db)

	// ── 服务层 ───────────────────────────────────────────────
	advSvc := advertiser_billing.NewService(advRepo, rdb, m)
	pubSvc := publisher_billing.NewService(pubRepo, m)
	platSvc := platform.NewService(platRepo)

	// ── HTTP ──────────────────────────────────────────────────
	mux := http.NewServeMux()
	api.NewHandler(advSvc, pubSvc, platSvc).RegisterRoutes(mux)
	mux.Handle("/metrics", metrics.Handler(reg))

	srv := &http.Server{
		Addr:         ":8085",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Kafka Consumer ────────────────────────────────────────
	brokers := []string{getenv("KAFKA_BROKERS", "localhost:9092")}
	c := consumer.NewConsumer(brokers, advSvc, pubSvc, platSvc, m)
	go func() {
		if err := c.Start(ctx); err != nil {
			slog.Error("kafka consumer error", "err", err)
		}
	}()

	// ── 每日结算任务 ───────────────────────────────────────────
	job := platform.NewSettlementJob(platSvc)
	go job.Start(ctx)

	// ── 启动 HTTP ─────────────────────────────────────────────
	go func() {
		slog.Info("billing service started", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "err", err)
		}
	}()

	// ── 优雅关机 ──────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("http shutdown error", "err", err)
	}
	slog.Info("billing service stopped")
}

func getenv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
