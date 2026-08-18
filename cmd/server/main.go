package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gophprofile/avatars-service/internal/api"
	"github.com/gophprofile/avatars-service/internal/config"
	"github.com/gophprofile/avatars-service/internal/migrate"
	"github.com/gophprofile/avatars-service/internal/observability"
	"github.com/gophprofile/avatars-service/internal/repository"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logger := observability.InitLogger(cfg.OTelServiceName, cfg.LogLevel)

	obsShutdown, err := observability.Init(cfg.OTelServiceName, cfg.OTelExporterAddr, cfg.MetricsPort)
	if err != nil {
		logger.Error("init observability", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := obsShutdown(shutCtx); err != nil {
			logger.Error("shutdown observability", "error", err)
		}
	}()

	metricsServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler:           observability.PrometheusHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("metrics server starting", "port", cfg.MetricsPort)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", "error", err)
		}
	}()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		metricsServer.Shutdown(shutCtx)
	}()

	db, err := sqlx.Connect("postgres", cfg.DBDSN())
	if err != nil {
		logger.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		logger.Error("ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	err = migrate.Up(cfg.DBDSN())
	if err != nil {
		logger.Error("run migrations", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	s3Client, err := services.NewS3Service(ctx, cfg.S3EndpointWithScheme(), cfg.S3Region, cfg.S3Bucket, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3UsePathStyle)
	if err != nil {
		logger.Error("create s3 client", "error", err)
		os.Exit(1)
	}

	rmq, err := services.NewRabbitMQPublisher(ctx, cfg.RMQURL, cfg.RMQExchange)
	if err != nil {
		logger.Error("create rabbitmq publisher", "error", err)
		os.Exit(1)
	}
	defer rmq.Close()

	repo := repository.NewAvatarRepository(db)
	mimeValidator := services.NewMIMEValidator()

	svc := services.NewAvatarService(repo, s3Client, rmq, mimeValidator, cfg.MaxUploadSize)

	router := api.NewRouter(cfg, db.DB, svc, rmq, s3Client)

	errCh := make(chan error, 1)
	go func() {
		errCh <- router.Start(cfg)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		logger.Info("shutting down server...")
	case err := <-errCh:
		logger.Error("server error", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := router.Shutdown(ctx); err != nil {
		logger.Error("server forced shutdown", "error", err)
	}
	logger.Info("server exited properly")
}
