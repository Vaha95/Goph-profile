package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gophprofile/avatars-service/internal/api"
	"github.com/gophprofile/avatars-service/internal/config"
	"github.com/gophprofile/avatars-service/internal/migrate"
	"github.com/gophprofile/avatars-service/internal/repository"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := sqlx.Connect("postgres", cfg.DBDSN())
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatalf("ping database: %v", err)
	}
	log.Printf("connected to database")

	err = migrate.Up(cfg.DBDSN())
	if err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	ctx := context.Background()
	s3Client, err := services.NewS3Service(ctx, cfg.S3EndpointWithScheme(), cfg.S3Region, cfg.S3Bucket, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3UsePathStyle)
	if err != nil {
		log.Fatalf("create s3 client: %v", err)
	}

	rmq, err := services.NewRabbitMQPublisher(ctx, cfg.RMQURL, cfg.RMQExchange)
	if err != nil {
		log.Fatalf("create rabbitmq publisher: %v", err)
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
		log.Println("shutting down server...")
	case err := <-errCh:
		log.Printf("server error: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := router.Shutdown(ctx); err != nil {
		log.Printf("server forced shutdown: %v", err)
	}
	log.Println("server exited properly")
}
