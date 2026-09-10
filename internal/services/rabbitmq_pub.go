package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gophprofile/avatars-service/internal/observability"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const RoutingKeyUpload = "avatar.uploaded"

type RabbitMQPublisher interface {
	PublishUploadEvent(ctx context.Context, event map[string]any) error
	Close() error
	IsConnected() bool
}

type rabbitMQPublisher struct {
	url      string
	exchange string

	mu      sync.Mutex
	conn    *amqp.Connection
	ch      *amqp.Channel
	metrics *observability.Metrics
}

func NewRabbitMQPublisher(ctx context.Context, url, exchange string) (RabbitMQPublisher, error) {
	p := &rabbitMQPublisher{
		url:      url,
		exchange: exchange,
		metrics:  observability.NewMetricsFromGlobal(),
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.connect(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *rabbitMQPublisher) connect(ctx context.Context) error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("rabbitmq connect: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("rabbitmq channel: %w", err)
	}

	err = ch.ExchangeDeclare(
		p.exchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("rabbitmq exchange declare: %w", err)
	}

	p.conn = conn
	p.ch = ch
	return nil
}

func (p *rabbitMQPublisher) ensureConnectedLocked() error {
	if p.conn != nil && p.ch != nil && !p.conn.IsClosed() && !p.ch.IsClosed() {
		return nil
	}
	return p.connect(context.Background())
}

func (p *rabbitMQPublisher) publish(ctx context.Context, routingKey string, v any) error {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("rmq-publisher")
	ctx, span := tracer.Start(ctx, "rmq.publish",
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination", p.exchange),
			attribute.String("messaging.routing_key", routingKey),
		),
	)
	defer span.End()

	start := time.Now()

	body, err := json.Marshal(v)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("marshal event: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureConnectedLocked(); err != nil {
		span.RecordError(err)
		return fmt.Errorf("publish event: %w", err)
	}

	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = p.ch.PublishWithContext(ctx,
			p.exchange,
			routingKey,
			true,
			false,
			amqp.Publishing{
				ContentType:  "application/json",
				Body:         body,
				DeliveryMode: amqp.Persistent,
				Timestamp:    time.Now(),
			},
		)
		if lastErr == nil {
			span.SetAttributes(
				attribute.Int("messaging.message.size", len(body)),
				attribute.Float64("messaging.duration_ms", float64(time.Since(start).Milliseconds())),
			)
			if p.metrics != nil {
				observability.RecordRMQPublish(ctx, p.metrics, routingKey, time.Since(start).Seconds(), false)
			}
			return nil
		}

		if p.conn.IsClosed() || p.ch.IsClosed() {
			if cerr := p.connect(ctx); cerr != nil {
				span.RecordError(cerr)
				if p.metrics != nil {
					observability.RecordRMQPublish(ctx, p.metrics, routingKey, time.Since(start).Seconds(), true)
				}
				return fmt.Errorf("publish event (reconnect failed): %w", cerr)
			}
		}

		select {
		case <-ctx.Done():
			span.RecordError(ctx.Err())
			if p.metrics != nil {
				observability.RecordRMQPublish(ctx, p.metrics, routingKey, time.Since(start).Seconds(), true)
			}
			return fmt.Errorf("publish event cancelled: %w", ctx.Err())
		case <-time.After(time.Duration(i+1) * 100 * time.Millisecond):
		}
	}

	span.RecordError(lastErr)
	span.SetAttributes(attribute.Int("messaging.retry_count", 3))
	if p.metrics != nil {
		observability.RecordRMQPublish(ctx, p.metrics, routingKey, time.Since(start).Seconds(), true)
	}
	return fmt.Errorf("publish event (after 3 attempts): %w", lastErr)
}

func (p *rabbitMQPublisher) PublishUploadEvent(ctx context.Context, event map[string]any) error {
	return p.publish(ctx, RoutingKeyUpload, event)
}

func (p *rabbitMQPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ch != nil {
		p.ch.Close()
		p.ch = nil
	}
	if p.conn != nil {
		err := p.conn.Close()
		p.conn = nil
		return err
	}
	return nil
}

func (p *rabbitMQPublisher) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn != nil && !p.conn.IsClosed()
}
