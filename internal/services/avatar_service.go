package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/repository"
)

// ErrForbidden is returned when a user tries to modify an avatar they do not own.
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
}

func NewAvatarService(
	repo repository.AvatarRepository,
	s3 S3Service,
	rmq RabbitMQPublisher,
	mime MIMEValidator,
	maxSize int64,
) *AvatarService {
	return &AvatarService{
		repo:    repo,
		s3:      s3,
		rmq:     rmq,
		mime:    mime,
		maxSize: maxSize,
	}
}

func (s *AvatarService) UploadAvatar(ctx context.Context, userID string, file multipart.File, header *multipart.FileHeader) (*domain.Avatar, error) {
	mimeType, size, err := s.mime.Validate(file, s.maxSize)
	if err != nil {
		return nil, fmt.Errorf("validate file: %w", err)
	}

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
		return nil, fmt.Errorf("rewind file: %w", err)
	}

	err = s.s3.Upload(ctx, s3Key, file, size, mimeType)
	if err != nil {
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
		if derr := s.s3.Delete(ctx, []string{s3Key}); derr != nil {
			log.Printf("failed to rollback s3 upload for %s: %v", s3Key, derr)
		}
		return nil, fmt.Errorf("create avatar record: %w", err)
	}

	err = s.rmq.PublishUploadEvent(ctx, map[string]any{
		"avatar_id": avatar.ID.String(),
		"user_id":   avatar.UserID,
		"s3_key":    s3Key,
	})
	if err != nil {
		if rerr := s.repo.SoftDelete(ctx, avatarID); rerr != nil {
			log.Printf("failed to rollback avatar record %s: %v", avatarID, rerr)
		}
		if derr := s.s3.Delete(ctx, []string{s3Key}); derr != nil {
			log.Printf("failed to rollback s3 upload for %s: %v", s3Key, derr)
		}
		return nil, fmt.Errorf("publish upload event: %w", err)
	}

	return avatar, nil
}

func (s *AvatarService) GetAvatar(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get avatar: %w", err)
	}
	return avatar, nil
}

func (s *AvatarService) GetAvatarImage(ctx context.Context, id uuid.UUID, opts ImageOptions) (io.ReadCloser, string, error) {
	avatar, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, "", fmt.Errorf("get avatar: %w", err)
	}

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
		return nil, "", fmt.Errorf("download from s3: %w", err)
	}

	return reader, contentType, nil
}

func (s *AvatarService) DeleteAvatar(ctx context.Context, id uuid.UUID, owner string) error {
	avatar, err := s.repo.GetDeletedInfo(ctx, id)
	if err != nil {
		return fmt.Errorf("get avatar for deletion: %w", err)
	}

	if owner != "" && owner != avatar.UserID {
		return ErrForbidden
	}

	keysToDelete := []string{avatar.S3Key}
	for _, key := range avatar.ThumbnailS3Keys {
		keysToDelete = append(keysToDelete, key)
	}

	err = s.s3.Delete(ctx, keysToDelete)
	if err != nil {
		return fmt.Errorf("delete from s3: %w", err)
	}

	if owner != "" {
		err = s.repo.SoftDeleteOwned(ctx, id, owner)
	} else {
		err = s.repo.SoftDelete(ctx, id)
	}
	if err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}

	return nil
}

// DeleteLatestAvatarByUser deletes the latest avatar for the given user.
// It avoids the extra GetLatestByUserID round-trip by using a single SQL
// update with a subquery and fetches the S3 keys only for cleanup.
func (s *AvatarService) DeleteLatestAvatarByUser(ctx context.Context, userID string, requester string) error {
	if requester != "" && requester != userID {
		return ErrForbidden
	}

	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get latest avatar: %w", err)
	}

	keysToDelete := []string{avatar.S3Key}
	for _, key := range avatar.ThumbnailS3Keys {
		keysToDelete = append(keysToDelete, key)
	}

	err = s.s3.Delete(ctx, keysToDelete)
	if err != nil {
		return fmt.Errorf("delete from s3: %w", err)
	}

	err = s.repo.SoftDeleteLatestOwnedByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}

	return nil
}

func (s *AvatarService) GetLatestAvatarByUser(ctx context.Context, userID string) (*domain.Avatar, error) {
	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get latest avatar: %w", err)
	}
	return avatar, nil
}

func (s *AvatarService) ListAvatarsByUser(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
	avatars, err := s.repo.ListByUserID(ctx, userID, limit, offset)
	if err != nil {
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
