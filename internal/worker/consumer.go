package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/observability"
	"github.com/gophprofile/avatars-service/internal/repository"
	"github.com/gophprofile/avatars-service/internal/services"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	bindingKey        = "avatar.uploaded"
	retryCountHeader  = "x-retry-count"
	maxMessageRetries = 5
	baseRetryDelay    = 10 * time.Second
	maxImageDimension = 10000
)

type Consumer struct {
	rmqURL    string
	exchange  string
	queue     string
	s3        services.S3Service
	repo      repository.AvatarRepository
	thumb     Thumbnailer
	metrics   *observability.Metrics
	connected bool
	mu        sync.Mutex
}

type ConsumerConfig struct {
	RMQURL   string
	Exchange string
	Queue    string
}

func NewConsumer(cfg ConsumerConfig, s3 services.S3Service, repo repository.AvatarRepository, thumb Thumbnailer) *Consumer {
	m := observability.NewMetricsFromGlobal()
	return &Consumer{
		rmqURL:   cfg.RMQURL,
		exchange: cfg.Exchange,
		queue:    cfg.Queue,
		s3:       s3,
		repo:     repo,
		thumb:    thumb,
		metrics:  m,
	}
}

func (c *Consumer) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, ch, err := c.connect(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "RabbitMQ setup failed, retrying...", "error", err)
			if !sleepOrCancel(ctx, 5*time.Second) {
				return ctx.Err()
			}
			continue
		}

		msgs, err := ch.Consume(
			c.queue,
			"",
			false,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			ch.Close()
			conn.Close()
			slog.ErrorContext(ctx, "consume failed, retrying...", "error", err)
			if !sleepOrCancel(ctx, 5*time.Second) {
				return ctx.Err()
			}
			continue
		}

		c.mu.Lock()
		c.connected = true
		c.mu.Unlock()

		slog.InfoContext(ctx, "worker started, consuming from queue", "queue", c.queue)

		notifyClose := conn.NotifyClose(make(chan *amqp.Error, 1))
		go c.handleMessages(ctx, ch, msgs)

		select {
		case <-ctx.Done():
			ch.Close()
			conn.Close()
			return ctx.Err()
		case connErr := <-notifyClose:
			slog.WarnContext(ctx, "RabbitMQ connection lost, reconnecting...", "error", connErr)
			ch.Close()
			conn.Close()
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			if !sleepOrCancel(ctx, 5*time.Second) {
				return ctx.Err()
			}
		}
	}
}

func (c *Consumer) connect(ctx context.Context) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.Dial(c.rmqURL)
	if err != nil {
		return nil, nil, fmt.Errorf("rabbitmq connect: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq channel: %w", err)
	}

	closeAll := func() { ch.Close(); conn.Close() }

	if err := ch.ExchangeDeclare(c.exchange, "topic", true, false, false, false, nil); err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("exchange declare: %w", err)
	}

	dlqName := c.queue + ".dlq"
	if _, err := ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("dlq declare: %w", err)
	}

	q, err := ch.QueueDeclare(
		c.queue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": dlqName,
		},
	)
	if err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("queue declare: %w", err)
	}

	if err := ch.QueueBind(q.Name, bindingKey, c.exchange, false, nil); err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("queue bind: %w", err)
	}

	retryQueue := c.queue + ".retry"
	retryQueueArgs := amqp.Table{
		"x-dead-letter-exchange":    c.exchange,
		"x-dead-letter-routing-key": bindingKey,
	}
	if _, err := ch.QueueDeclare(retryQueue, true, false, false, false, retryQueueArgs); err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("retry queue declare: %w", err)
	}

	if err := ch.Qos(1, 0, false); err != nil {
		closeAll()
		return nil, nil, fmt.Errorf("qos: %w", err)
	}

	return conn, ch, nil
}

func sleepOrCancel(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (c *Consumer) handleMessages(ctx context.Context, ch *amqp.Channel, msgs <-chan amqp.Delivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			c.processMessage(ctx, ch, msg)
		}
	}
}

func (c *Consumer) processMessage(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("rmq-consumer")
	ctx, span := tracer.Start(ctx, "rmq.consume.message",
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination", c.queue),
			attribute.Int64("messaging.message.delivery_tag", int64(msg.DeliveryTag)),
		),
	)
	defer span.End()

	start := time.Now()

	var event struct {
		AvatarID       string `json:"avatar_id"`
		UserID         string `json:"user_id"`
		S3Key          string `json:"s3_key"`
		IdempotencyKey string `json:"idempotency_key"`
	}

	if err := json.Unmarshal(msg.Body, &event); err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("failed to unmarshal event, moving to DLQ", "error", err)
		msg.Nack(false, false)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	span.SetAttributes(
		attribute.String("avatar_id", event.AvatarID),
		attribute.String("user_id", event.UserID),
		attribute.String("s3_key", event.S3Key),
	)

	if event.IdempotencyKey == "" {
		event.IdempotencyKey = event.AvatarID + ":" + event.S3Key
	}

	avatarID, err := uuid.Parse(event.AvatarID)
	if err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("invalid avatar ID, moving to DLQ", "avatar_id", event.AvatarID, "error", err)
		msg.Nack(false, false)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	err = c.repo.ClaimProcessing(ctx, avatarID, event.IdempotencyKey)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicateKey) {
			span.SetAttributes(attribute.Bool("duplicate", true))
			observability.L(ctx).Info("avatar already claimed, acking", "avatar_id", event.AvatarID)
			msg.Ack(false)
			observability.RecordRMQConsume(ctx, c.metrics, "duplicate", time.Since(start).Seconds())
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			span.SetAttributes(attribute.Bool("not_found", true))
			observability.L(ctx).Info("avatar not found, dropping message", "avatar_id", event.AvatarID)
			msg.Ack(false)
			observability.RecordRMQConsume(ctx, c.metrics, "not_found", time.Since(start).Seconds())
			return
		}
		span.RecordError(err)
		observability.L(ctx).Error("failed to claim avatar", "avatar_id", event.AvatarID, "error", err)
		c.retryOrDLQ(ctx, ch, msg, event.AvatarID)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	reader, err := c.s3.Download(ctx, event.S3Key)
	if err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("failed to download image", "error", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("failed to read image data", "error", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	if err := CheckImageDimensions(data, maxImageDimension); err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("image rejected before decode", "error", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	srcImg, _, err := DecodeImage(data)
	if err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("failed to decode image", "error", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	if srcImg.Bounds().Dx() > maxImageDimension || srcImg.Bounds().Dy() > maxImageDimension {
		err := fmt.Errorf("image too large %dx%d (max %d)", srcImg.Bounds().Dx(), srcImg.Bounds().Dy(), maxImageDimension)
		span.RecordError(err)
		observability.L(ctx).Error("image too large",
			"width", srcImg.Bounds().Dx(),
			"height", srcImg.Bounds().Dy(),
			"max", maxImageDimension,
		)
		c.failAndAck(ctx, avatarID, msg, nil)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	thumbnails, err := c.thumb.Generate(ctx, srcImg, "image/jpeg", domain.DefaultThumbnailSizes)
	if err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("failed to generate thumbnails", "error", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	thumbnailKeys := make(map[string]string)
	uploadedKeys := make([]string, 0, len(thumbnails))
	for size, thumbData := range thumbnails {
		key := fmt.Sprintf("%s/%s/%s.jpg", event.UserID, event.AvatarID, size)
		if err := c.s3.Upload(ctx, key, bytes.NewReader(thumbData), int64(len(thumbData)), "image/jpeg"); err != nil {
			span.RecordError(err)
			observability.L(ctx).Error("failed to upload thumbnail", "size", size, "error", err)
			c.failAndAck(ctx, avatarID, msg, uploadedKeys)
			observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
			return
		}
		uploadedKeys = append(uploadedKeys, key)
		thumbnailKeys[size] = key
	}

	if err := c.repo.UpdateThumbnailKeys(ctx, avatarID, thumbnailKeys); err != nil {
		span.RecordError(err)
		observability.L(ctx).Error("failed to update thumbnail keys", "avatar_id", event.AvatarID, "error", err)
		c.retryOrDLQ(ctx, ch, msg, event.AvatarID)
		observability.RecordRMQConsume(ctx, c.metrics, "error", time.Since(start).Seconds())
		return
	}

	span.SetAttributes(
		attribute.Int("thumbnails.count", len(thumbnails)),
		attribute.Float64("processing.duration_ms", float64(time.Since(start).Milliseconds())),
	)
	observability.L(ctx).Info("avatar processed successfully", "avatar_id", event.AvatarID, "duration_ms", time.Since(start).Milliseconds())
	msg.Ack(false)
	observability.RecordRMQConsume(ctx, c.metrics, "success", time.Since(start).Seconds())
}

func (c *Consumer) failAndAck(ctx context.Context, avatarID uuid.UUID, msg amqp.Delivery, uploadedKeys []string) {
	if len(uploadedKeys) > 0 {
		if err := c.s3.Delete(ctx, uploadedKeys); err != nil {
			observability.L(ctx).Error("failed to clean up orphaned thumbnails", "avatar_id", avatarID, "error", err)
		}
	}
	if err := c.repo.UpdateProcessingStatus(ctx, avatarID, domain.ProcessingFailed); err != nil {
		observability.L(ctx).Error("failed to mark avatar as failed", "avatar_id", avatarID, "error", err)
	}
	msg.Ack(false)
}

func (c *Consumer) retryOrDLQ(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery, avatarID string) {
	defer func() {
		if rec := recover(); rec != nil {
			observability.L(ctx).Error("panic in retryOrDLQ, sending to DLQ", "avatar_id", avatarID, "panic", rec)
			msg.Nack(false, false)
		}
	}()
	retries := retryCount(msg)
	if retries >= maxMessageRetries {
		observability.L(ctx).Warn("retries exhausted, moving to DLQ", "avatar_id", avatarID)
		msg.Nack(false, false)
		return
	}

	retries++
	headers := msg.Headers
	if headers == nil {
		headers = amqp.Table{}
	}
	headers[retryCountHeader] = int32(retries)

	delay := baseRetryDelay * time.Duration(1<<uint(retries-1))

	err := ch.PublishWithContext(ctx, "", c.queue+".retry", false, false, amqp.Publishing{
		ContentType:  msg.ContentType,
		Body:         msg.Body,
		Headers:      headers,
		DeliveryMode: msg.DeliveryMode,
		Timestamp:    time.Now(),
		Expiration:   fmt.Sprintf("%d", delay.Milliseconds()),
	})
	if err != nil {
		observability.L(ctx).Error("failed to publish retry, moving to DLQ", "avatar_id", avatarID, "error", err)
		msg.Nack(false, false)
		return
	}
	msg.Ack(false)
}

func retryCount(msg amqp.Delivery) int {
	if msg.Headers == nil {
		return 0
	}
	if v, ok := msg.Headers[retryCountHeader].(int32); ok {
		return int(v)
	}
	return 0
}

func (c *Consumer) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}
