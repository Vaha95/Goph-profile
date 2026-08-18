package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/gophprofile/avatars-service/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type S3Service interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, keys []string) error
	BucketExists(ctx context.Context) (bool, error)
	PresignGetURL(ctx context.Context, key string, expiresIn time.Duration) (string, error)
}

type s3Service struct {
	client  *s3.Client
	bucket  string
	metrics *observability.Metrics
}

func NewS3Service(ctx context.Context, endpoint, region, bucket, accessKey, secretKey string, usePathStyle bool) (S3Service, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		config.WithBaseEndpoint(endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = usePathStyle
	})

	return &s3Service{client: client, bucket: bucket, metrics: observability.NewMetricsFromGlobal()}, nil
}

func (s *s3Service) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("s3-service")
	ctx, span := tracer.Start(ctx, "s3.Upload",
		trace.WithAttributes(
			attribute.String("s3.key", key),
			attribute.String("s3.bucket", s.bucket),
			attribute.Int64("s3.content_length", size),
			attribute.String("s3.content_type", contentType),
		),
	)
	defer span.End()

	start := time.Now()

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        reader,
		ContentType: aws.String(contentType),
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	}
	span.SetAttributes(attribute.Float64("s3.duration_ms", float64(time.Since(start).Milliseconds())))
	if s.metrics != nil {
		observability.RecordS3Operation(ctx, s.metrics, "Upload", time.Since(start).Seconds(), err != nil)
	}
	return err
}

func (s *s3Service) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("s3-service")
	ctx, span := tracer.Start(ctx, "s3.Download",
		trace.WithAttributes(
			attribute.String("s3.key", key),
			attribute.String("s3.bucket", s.bucket),
		),
	)
	defer span.End()

	start := time.Now()

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}

	result, err := s.client.GetObject(ctx, input)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
		if s.metrics != nil {
			observability.RecordS3Operation(ctx, s.metrics, "Download", time.Since(start).Seconds(), true)
		}
		return nil, err
	}
	span.SetAttributes(attribute.Float64("s3.duration_ms", float64(time.Since(start).Milliseconds())))
	if s.metrics != nil {
		observability.RecordS3Operation(ctx, s.metrics, "Download", time.Since(start).Seconds(), false)
	}
	return result.Body, nil
}

func (s *s3Service) Delete(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("s3-service")
	ctx, span := tracer.Start(ctx, "s3.Delete",
		trace.WithAttributes(
			attribute.Int("s3.keys_count", len(keys)),
			attribute.String("s3.bucket", s.bucket),
		),
	)
	defer span.End()

	start := time.Now()

	objs := make([]types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		objs = append(objs, types.ObjectIdentifier{Key: aws.String(k)})
	}

	input := &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{
			Objects: objs,
			Quiet:   aws.Bool(true),
		},
	}

	_, err := s.client.DeleteObjects(ctx, input)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	}
	span.SetAttributes(attribute.Float64("s3.duration_ms", float64(time.Since(start).Milliseconds())))
	if s.metrics != nil {
		observability.RecordS3Operation(ctx, s.metrics, "Delete", time.Since(start).Seconds(), err != nil)
	}
	return err
}

func (s *s3Service) BucketExists(ctx context.Context) (bool, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("s3-service")
	ctx, span := tracer.Start(ctx, "s3.BucketExists",
		trace.WithAttributes(attribute.String("s3.bucket", s.bucket)),
	)
	defer span.End()

	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
			return false, nil
		}
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
		return false, fmt.Errorf("s3 head bucket: %w", err)
	}

	return true, nil
}

func (s *s3Service) PresignGetURL(ctx context.Context, key string, expiresIn time.Duration) (string, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("s3-service")
	ctx, span := tracer.Start(ctx, "s3.PresignGetURL",
		trace.WithAttributes(
			attribute.String("s3.key", key),
			attribute.String("s3.bucket", s.bucket),
		),
	)
	defer span.End()

	presignClient := s3.NewPresignClient(s.client)

	req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiresIn
	})
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("presign object: %w", err)
	}

	return req.URL, nil
}
