package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Metrics struct {
	UploadsTotal        metric.Int64Counter
	UploadDuration      metric.Float64Histogram
	StorageBytes        metric.Int64UpDownCounter
	DBQueryDuration     metric.Float64Histogram
	S3OperationDuration metric.Float64Histogram
	RMQPublishDuration  metric.Float64Histogram
	RMQConsumeDuration  metric.Float64Histogram
	RMQMessagesTotal    metric.Int64Counter
	QueueDepth          metric.Int64Gauge
}

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	uploadsTotal, err := meter.Int64Counter(
		"avatars.uploads.total",
		metric.WithDescription("Total number of avatar uploads"),
	)
	if err != nil {
		return nil, err
	}

	uploadDuration, err := meter.Float64Histogram(
		"avatars.upload.duration",
		metric.WithDescription("Avatar upload duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	storageBytes, err := meter.Int64UpDownCounter(
		"avatars.storage.bytes",
		metric.WithDescription("Total storage used by avatars in bytes"),
	)
	if err != nil {
		return nil, err
	}

	dbQueryDuration, err := meter.Float64Histogram(
		"avatars.db.query.duration",
		metric.WithDescription("Database query duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	s3OperationDuration, err := meter.Float64Histogram(
		"avatars.s3.operation.duration",
		metric.WithDescription("S3 operation duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	rmqPublishDuration, err := meter.Float64Histogram(
		"avatars.rmq.publish.duration",
		metric.WithDescription("RabbitMQ publish duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	rmqConsumeDuration, err := meter.Float64Histogram(
		"avatars.rmq.consume.duration",
		metric.WithDescription("RabbitMQ consume duration in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	rmqMessagesTotal, err := meter.Int64Counter(
		"avatars.rmq.messages.total",
		metric.WithDescription("Total number of RabbitMQ messages processed"),
	)
	if err != nil {
		return nil, err
	}

	queueDepth, err := meter.Int64Gauge(
		"avatars.rmq.queue.depth",
		metric.WithDescription("Current queue depth"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		UploadsTotal:        uploadsTotal,
		UploadDuration:      uploadDuration,
		StorageBytes:        storageBytes,
		DBQueryDuration:     dbQueryDuration,
		S3OperationDuration: s3OperationDuration,
		RMQPublishDuration:  rmqPublishDuration,
		RMQConsumeDuration:  rmqConsumeDuration,
		RMQMessagesTotal:    rmqMessagesTotal,
		QueueDepth:          queueDepth,
	}, nil
}

func RecordUpload(ctx context.Context, m *Metrics, status string, userID string, sizeBytes int64) {
	if m == nil {
		return
	}
	m.UploadsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", status),
		attribute.String("user_id", userID),
	))
	m.StorageBytes.Add(ctx, sizeBytes, metric.WithAttributes(
		attribute.String("user_id", userID),
	))
}

func RecordDBQuery(ctx context.Context, m *Metrics, operation string, duration float64, err bool) {
	if m == nil {
		return
	}
	m.DBQueryDuration.Record(ctx, duration, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("error", err),
	))
}

func RecordS3Operation(ctx context.Context, m *Metrics, operation string, duration float64, err bool) {
	if m == nil {
		return
	}
	m.S3OperationDuration.Record(ctx, duration, metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("error", err),
	))
}

func RecordRMQPublish(ctx context.Context, m *Metrics, routingKey string, duration float64, err bool) {
	if m == nil {
		return
	}
	m.RMQPublishDuration.Record(ctx, duration, metric.WithAttributes(
		attribute.String("routing_key", routingKey),
		attribute.Bool("error", err),
	))
}

func RecordRMQConsume(ctx context.Context, m *Metrics, status string, duration float64) {
	if m == nil {
		return
	}
	m.RMQConsumeDuration.Record(ctx, duration, metric.WithAttributes(
		attribute.String("status", status),
	))
	m.RMQMessagesTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", status),
	))
}

// NewMetricsFromGlobal creates a Metrics instance from the global OTel MeterProvider.
// Returns nil if the MeterProvider is not configured (e.g. during tests).
func NewMetricsFromGlobal() *Metrics {
	meter := otel.Meter("avatars-service")
	m, err := NewMetrics(meter)
	if err != nil {
		return nil
	}
	return m
}
