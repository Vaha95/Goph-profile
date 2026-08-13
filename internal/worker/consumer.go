package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/repository"
	"github.com/gophprofile/avatars-service/internal/services"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	bindingKey        = "avatar.uploaded"
	retryCountHeader  = "x-retry-count"
	maxMessageRetries = 5
	maxImageDimension = 10000
)

type Consumer struct {
	rmqURL    string
	exchange  string
	queue     string
	s3        services.S3Service
	repo      repository.AvatarRepository
	thumb     Thumbnailer
	connected bool
	mu        sync.Mutex
}

type ConsumerConfig struct {
	RMQURL   string
	Exchange string
	Queue    string
}

func NewConsumer(cfg ConsumerConfig, s3 services.S3Service, repo repository.AvatarRepository, thumb Thumbnailer) *Consumer {
	return &Consumer{
		rmqURL:   cfg.RMQURL,
		exchange: cfg.Exchange,
		queue:    cfg.Queue,
		s3:       s3,
		repo:     repo,
		thumb:    thumb,
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
			log.Printf("rabbitmq setup failed: %v, retrying...", err)
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
			log.Printf("consume failed: %v, retrying...", err)
			if !sleepOrCancel(ctx, 5*time.Second) {
				return ctx.Err()
			}
			continue
		}

		c.mu.Lock()
		c.connected = true
		c.mu.Unlock()

		log.Printf("worker started, consuming from queue %s", c.queue)

		notifyClose := conn.NotifyClose(make(chan *amqp.Error, 1))
		go c.handleMessages(ctx, ch, msgs)

		select {
		case <-ctx.Done():
			ch.Close()
			conn.Close()
			return ctx.Err()
		case connErr := <-notifyClose:
			log.Printf("rabbitmq connection lost: %v, reconnecting...", connErr)
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

// connect dials RabbitMQ and sets up the exchange, the main queue with a
// dead-letter queue, binding and prefetch.
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

	// Dead messages (unparsable, retries exhausted) are routed to the DLQ via
	// the default exchange.
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
	var event struct {
		AvatarID string `json:"avatar_id"`
		UserID   string `json:"user_id"`
		S3Key    string `json:"s3_key"`
	}

	if err := json.Unmarshal(msg.Body, &event); err != nil {
		log.Printf("failed to unmarshal event, moving to DLQ: %v", err)
		msg.Nack(false, false)
		return
	}

	avatarID, err := uuid.Parse(event.AvatarID)
	if err != nil {
		log.Printf("invalid avatar ID %q, moving to DLQ: %v", event.AvatarID, err)
		msg.Nack(false, false)
		return
	}

	avatar, err := c.repo.GetByID(ctx, avatarID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// The avatar no longer exists (deleted before processing): nothing to do.
			log.Printf("avatar %s not found, dropping message", event.AvatarID)
			msg.Ack(false)
			return
		}
		log.Printf("failed to get avatar %s: %v", event.AvatarID, err)
		c.retryOrDLQ(ctx, ch, msg, event.AvatarID)
		return
	}

	if avatar.ProcessingStatus == domain.ProcessingComplete {
		log.Printf("avatar %s already processed, skipping", event.AvatarID)
		msg.Ack(false)
		return
	}

	if err := c.repo.UpdateProcessingStatus(ctx, avatarID, domain.ProcessingInProgress); err != nil {
		log.Printf("failed to update processing status for %s: %v", event.AvatarID, err)
		c.retryOrDLQ(ctx, ch, msg, event.AvatarID)
		return
	}

	reader, err := c.s3.Download(ctx, event.S3Key)
	if err != nil {
		log.Printf("failed to download image: %v", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		return
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		log.Printf("failed to read image data: %v", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		return
	}

	if err := CheckImageDimensions(data, maxImageDimension); err != nil {
		log.Printf("image rejected before decode: %v", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		return
	}

	srcImg, _, err := DecodeImage(data)
	if err != nil {
		log.Printf("failed to decode image: %v", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		return
	}

	if srcImg.Bounds().Dx() > maxImageDimension || srcImg.Bounds().Dy() > maxImageDimension {
		log.Printf("image too large %dx%d (max %d)", srcImg.Bounds().Dx(), srcImg.Bounds().Dy(), maxImageDimension)
		c.failAndAck(ctx, avatarID, msg, nil)
		return
	}

	thumbnails, err := c.thumb.Generate(ctx, srcImg, "image/jpeg", domain.DefaultThumbnailSizes)
	if err != nil {
		log.Printf("failed to generate thumbnails: %v", err)
		c.failAndAck(ctx, avatarID, msg, nil)
		return
	}

	thumbnailKeys := make(map[string]string)
	uploadedKeys := make([]string, 0, len(thumbnails))
	for size, thumbData := range thumbnails {
		key := fmt.Sprintf("%s/%s/%s.jpg", event.UserID, event.AvatarID, size)
		if err := c.s3.Upload(ctx, key, bytes.NewReader(thumbData), int64(len(thumbData)), "image/jpeg"); err != nil {
			log.Printf("failed to upload thumbnail %s: %v", size, err)
			c.failAndAck(ctx, avatarID, msg, uploadedKeys)
			return
		}
		uploadedKeys = append(uploadedKeys, key)
		thumbnailKeys[size] = key
	}

	if err := c.repo.UpdateThumbnailKeys(ctx, avatarID, thumbnailKeys); err != nil {
		log.Printf("failed to update thumbnail keys for %s: %v", event.AvatarID, err)
		c.retryOrDLQ(ctx, ch, msg, event.AvatarID)
		return
	}

	log.Printf("avatar %s processed successfully", event.AvatarID)
	msg.Ack(false)
}

// failAndAck marks the avatar as failed, cleans up any partially uploaded
// thumbnails and acks the message (the failure is terminal).
func (c *Consumer) failAndAck(ctx context.Context, avatarID uuid.UUID, msg amqp.Delivery, uploadedKeys []string) {
	if len(uploadedKeys) > 0 {
		if err := c.s3.Delete(ctx, uploadedKeys); err != nil {
			log.Printf("failed to clean up orphaned thumbnails for %s: %v", avatarID, err)
		}
	}
	if err := c.repo.UpdateProcessingStatus(ctx, avatarID, domain.ProcessingFailed); err != nil {
		log.Printf("failed to mark avatar %s as failed: %v", avatarID, err)
	}
	msg.Ack(false)
}

// retryOrDLQ re-queues the message with a bounded retry counter and backoff.
// Once the counter is exhausted the message is rejected and routed to the DLQ.
func (c *Consumer) retryOrDLQ(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery, avatarID string) {
	retries := retryCount(msg)
	if retries >= maxMessageRetries {
		log.Printf("retries exhausted for avatar %s, moving to DLQ", avatarID)
		msg.Nack(false, false)
		return
	}

	if !sleepOrCancel(ctx, time.Duration(retries+1)*time.Second) {
		msg.Nack(false, true)
		return
	}

	headers := msg.Headers
	if headers == nil {
		headers = amqp.Table{}
	}
	headers[retryCountHeader] = int32(retries + 1)

	// Re-publish with an incremented retry counter and ack the original, so
	// the retry count survives across redeliveries.
	err := ch.Publish(c.exchange, bindingKey, false, false, amqp.Publishing{
		ContentType:  msg.ContentType,
		Body:         msg.Body,
		Headers:      headers,
		DeliveryMode: msg.DeliveryMode,
		Timestamp:    time.Now(),
	})
	if err != nil {
		log.Printf("failed to re-publish retry for avatar %s, moving to DLQ: %v", avatarID, err)
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
