package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const RoutingKeyUpload = "avatar.uploaded"

type RabbitMQPublisher interface {
	PublishUploadEvent(ctx context.Context, event map[string]any) error
	Close() error
}

type rabbitMQPublisher struct {
	url      string
	exchange string

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewRabbitMQPublisher(ctx context.Context, url, exchange string) (RabbitMQPublisher, error) {
	p := &rabbitMQPublisher{url: url, exchange: exchange}
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

// ensureConnectedLocked re-establishes the connection if it is missing or closed.
// Callers must hold p.mu.
func (p *rabbitMQPublisher) ensureConnectedLocked() error {
	if p.conn != nil && p.ch != nil && !p.conn.IsClosed() && !p.ch.IsClosed() {
		return nil
	}
	return p.connect(context.Background())
}

func (p *rabbitMQPublisher) publish(ctx context.Context, routingKey string, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureConnectedLocked(); err != nil {
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
			return nil
		}

		// If the connection dropped, reconnect so the next attempt succeeds.
		if p.conn.IsClosed() || p.ch.IsClosed() {
			if cerr := p.connect(ctx); cerr != nil {
				return fmt.Errorf("publish event (reconnect failed): %w", cerr)
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("publish event cancelled: %w", ctx.Err())
		case <-time.After(time.Duration(i+1) * 100 * time.Millisecond):
		}
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
