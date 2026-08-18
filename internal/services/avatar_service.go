package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/observability"
	"github.com/gophprofile/avatars-service/internal/repository"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var ErrForbidden = errors.New("forbidden")

type ImageOptions struct {
	Size string
}

type AvatarService struct {
	repo    repository.AvatarRepository
	s3      S3Service
	rmq     RabbitMQPublisher
	mime    MIMEValidator
	maxSize int64
	metrics *observability.Metrics
}

func NewAvatarService(
	repo repository.AvatarRepository,
	s3 S3Service,
	rmq RabbitMQPublisher,
	mime MIMEValidator,
	maxSize int64,
) *AvatarService {
	var metrics *observability.Metrics
	if m := otel.Meter("avatars-service"); m != nil {
		metrics, _ = observability.NewMetrics(m)
	}
	return &AvatarService{
		repo:    repo,
		s3:      s3,
		rmq:     rmq,
		mime:    mime,
		maxSize: maxSize,
		metrics: metrics,
	}
}

func (s *AvatarService) UploadAvatar(ctx context.Context, userID string, file multipart.File, header *multipart.FileHeader) (*domain.Avatar, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "UploadAvatar",
		trace.WithAttributes(
			attribute.String("user_id", userID),
			attribute.String("file_name", header.Filename),
			attribute.Int64("file_size", header.Size),
		),
	)
	defer span.End()

	start := time.Now()

	mimeType, size, err := s.mime.Validate(file, s.maxSize)
	if err != nil {
		span.RecordError(err)
		observability.L(ctx).Warn("file validation failed", "user_id", userID, "error", err)
		observability.RecordUpload(ctx, s.metrics, "error", userID, 0)
		return nil, fmt.Errorf("validate file: %w", err)
	}

	observability.L(ctx).Info("uploading avatar",
		"user_id", userID,
		"file_size", size,
		"mime_type", mimeType,
	)

	avatarID := uuid.New()
	ext := mimeTypeToExt(mimeType)
	if ext == "" {
		ext = filepath.Ext(header.Filename)
		if ext == "" {
			ext = ".jpg"
		}
	}
	s3Key := fmt.Sprintf("%s/%s%s", userID, avatarID, ext)

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("rewind file: %w", err)
	}

	err = s.s3.Upload(ctx, s3Key, file, size, mimeType)
	if err != nil {
		span.RecordError(err)
		observability.RecordUpload(ctx, s.metrics, "error", userID, 0)
		return nil, fmt.Errorf("upload to s3: %w", err)
	}

	avatar := &domain.Avatar{
		ID:               avatarID,
		UserID:           userID,
		FileName:         header.Filename,
		MimeType:         mimeType,
		SizeBytes:        size,
		S3Key:            s3Key,
		ThumbnailS3Keys:  make(domain.JSONMap),
		UploadStatus:     domain.StatusUploaded,
		ProcessingStatus: domain.ProcessingPending,
	}

	err = s.repo.Create(ctx, avatar)
	if err != nil {
		s.cleanupUpload(ctx, s3Key, avatarID)
		span.RecordError(err)
		observability.RecordUpload(ctx, s.metrics, "error", userID, 0)
		return nil, fmt.Errorf("create avatar record: %w", err)
	}

	idKey := avatar.ID.String() + ":" + s3Key

	err = s.rmq.PublishUploadEvent(ctx, map[string]any{
		"avatar_id":       avatar.ID.String(),
		"user_id":         avatar.UserID,
		"s3_key":          s3Key,
		"idempotency_key": idKey,
	})
	if err != nil {
		s.cleanupUpload(ctx, s3Key, avatarID)
		span.RecordError(err)
		observability.RecordUpload(ctx, s.metrics, "error", userID, 0)
		return nil, fmt.Errorf("publish upload event: %w", err)
	}

	span.SetAttributes(
		attribute.String("avatar_id", avatarID.String()),
		attribute.Int64("size_bytes", size),
	)

	if s.metrics != nil {
		observability.RecordUpload(ctx, s.metrics, "success", userID, size)
		s.metrics.UploadDuration.Record(ctx, time.Since(start).Seconds())
	}

	return avatar, nil
}

func (s *AvatarService) cleanupUpload(ctx context.Context, s3Key string, avatarID uuid.UUID) {
	if rerr := s.repo.SoftDelete(ctx, avatarID); rerr != nil {
		observability.L(ctx).Error("failed to rollback avatar record", "avatar_id", avatarID, "error", rerr)
	}
	if derr := s.s3.Delete(ctx, []string{s3Key}); derr != nil {
		observability.L(ctx).Error("failed to delete s3 object", "s3_key", s3Key, "error", derr)
	}
}

func (s *AvatarService) GetAvatar(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "GetAvatar",
		trace.WithAttributes(attribute.String("avatar_id", id.String())),
	)
	defer span.End()

	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get avatar: %w", err)
	}
	return avatar, nil
}

func (s *AvatarService) GetAvatarImage(ctx context.Context, id uuid.UUID, opts ImageOptions) (io.ReadCloser, string, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "GetAvatarImage",
		trace.WithAttributes(
			attribute.String("avatar_id", id.String()),
			attribute.String("size", opts.Size),
		),
	)
	defer span.End()

	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		return nil, "", fmt.Errorf("get avatar: %w", err)
	}

	return s.resolveAvatarImage(ctx, avatar, opts, span)
}

func (s *AvatarService) GetAvatarImageWithAvatar(ctx context.Context, avatar *domain.Avatar, opts ImageOptions) (io.ReadCloser, string, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "GetAvatarImageWithAvatar",
		trace.WithAttributes(
			attribute.String("avatar_id", avatar.ID.String()),
			attribute.String("size", opts.Size),
		),
	)
	defer span.End()

	return s.resolveAvatarImage(ctx, avatar, opts, span)
}

func (s *AvatarService) resolveAvatarImage(ctx context.Context, avatar *domain.Avatar, opts ImageOptions, span trace.Span) (io.ReadCloser, string, error) {
	key := avatar.S3Key
	contentType := avatar.MimeType

	if opts.Size != "" && opts.Size != "original" {
		if thumbKey, ok := avatar.ThumbnailS3Keys[opts.Size]; ok {
			key = thumbKey
			contentType = "image/jpeg"
		}
	}

	reader, err := s.s3.Download(ctx, key)
	if err != nil {
		span.RecordError(err)
		return nil, "", fmt.Errorf("download from s3: %w", err)
	}

	return reader, contentType, nil
}

func (s *AvatarService) DeleteAvatar(ctx context.Context, id uuid.UUID, owner string) error {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "DeleteAvatar",
		trace.WithAttributes(
			attribute.String("avatar_id", id.String()),
			attribute.String("owner", owner),
		),
	)
	defer span.End()

	avatar, err := s.repo.GetDeletedInfo(ctx, id)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("get avatar for deletion: %w", err)
	}

	if owner != "" && owner != avatar.UserID {
		span.SetAttributes(attribute.Bool("forbidden", true))
		return ErrForbidden
	}

	keysToDelete := []string{avatar.S3Key}
	for _, key := range avatar.ThumbnailS3Keys {
		keysToDelete = append(keysToDelete, key)
	}

	err = s.s3.Delete(ctx, keysToDelete)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("delete from s3: %w", err)
	}

	if owner != "" {
		err = s.repo.SoftDeleteOwned(ctx, id, owner)
	} else {
		err = s.repo.SoftDelete(ctx, id)
	}
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("soft delete: %w", err)
	}

	return nil
}

func (s *AvatarService) DeleteLatestAvatarByUser(ctx context.Context, userID string, requester string) error {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "DeleteLatestAvatarByUser",
		trace.WithAttributes(
			attribute.String("user_id", userID),
			attribute.String("requester", requester),
		),
	)
	defer span.End()

	if requester != "" && requester != userID {
		span.SetAttributes(attribute.Bool("forbidden", true))
		return ErrForbidden
	}

	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("get latest avatar: %w", err)
	}

	keysToDelete := []string{avatar.S3Key}
	for _, key := range avatar.ThumbnailS3Keys {
		keysToDelete = append(keysToDelete, key)
	}

	err = s.s3.Delete(ctx, keysToDelete)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("delete from s3: %w", err)
	}

	err = s.repo.SoftDeleteLatestOwnedByUserID(ctx, userID)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("soft delete: %w", err)
	}

	return nil
}

func (s *AvatarService) GetLatestAvatarByUser(ctx context.Context, userID string) (*domain.Avatar, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "GetLatestAvatarByUser",
		trace.WithAttributes(attribute.String("user_id", userID)),
	)
	defer span.End()

	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get latest avatar: %w", err)
	}
	return avatar, nil
}

func (s *AvatarService) ListAvatarsByUser(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
	tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("avatar-service")
	ctx, span := tracer.Start(ctx, "ListAvatarsByUser",
		trace.WithAttributes(
			attribute.String("user_id", userID),
			attribute.Int("limit", limit),
			attribute.Int("offset", offset),
		),
	)
	defer span.End()

	avatars, err := s.repo.ListByUserID(ctx, userID, limit, offset)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list avatars: %w", err)
	}
	return avatars, nil
}

func mimeTypeToExt(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}
