package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gophprofile/avatars-service/internal/config"
	"github.com/gophprofile/avatars-service/internal/migrate"
	"github.com/gophprofile/avatars-service/internal/repository"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/gophprofile/avatars-service/internal/worker"
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

	repo := repository.NewAvatarRepository(db)
	thumb := worker.NewThumbnailer()

	consumer := worker.NewConsumer(
		worker.ConsumerConfig{
			RMQURL:   cfg.RMQURL,
			Exchange: cfg.RMQExchange,
			Queue:    cfg.RMQQueue,
		},
		s3Client,
		repo,
		thumb,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		err := consumer.Start(ctx)
		if err != nil {
			log.Printf("worker error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down worker...")
	cancel()
	log.Println("worker exited properly")
}
