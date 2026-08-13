package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"

	"github.com/jmoiron/sqlx"
)

// ErrNotFound is returned when the requested avatar does not exist (or has been deleted).
var ErrNotFound = errors.New("avatar not found")

type AvatarRepository interface {
	Create(ctx context.Context, avatar *domain.Avatar) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Avatar, error)
	GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error)
	ListByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error)
	UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateUploadStatus(ctx context.Context, id uuid.UUID, status string) error
	UpdateThumbnailKeys(ctx context.Context, id uuid.UUID, keys map[string]string) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	SoftDeleteByUserID(ctx context.Context, userID string) error
	SoftDeleteOwned(ctx context.Context, id uuid.UUID, userID string) error
	SoftDeleteLatestOwnedByUserID(ctx context.Context, userID string) error
	GetDeletedInfo(ctx context.Context, id uuid.UUID) (*domain.Avatar, error)
}

type avatarRepo struct {
	db *sqlx.DB
}

func NewAvatarRepository(db *sqlx.DB) AvatarRepository {
	return &avatarRepo{db: db}
}

func (r *avatarRepo) Create(ctx context.Context, avatar *domain.Avatar) error {
	query := `INSERT INTO avatars (id, user_id, file_name, mime_type, size_bytes, s3_key,
		thumbnail_s3_keys, upload_status, processing_status, created_at, updated_at)
		VALUES (:id, :user_id, :file_name, :mime_type, :size_bytes, :s3_key,
		:thumbnail_s3_keys, :upload_status, :processing_status, :created_at, :updated_at)`

	now := time.Now().UTC()
	if avatar.ID == uuid.Nil {
		avatar.ID = uuid.New()
	}
	avatar.CreatedAt = now
	avatar.UpdatedAt = now

	args := map[string]any{
		"id":                avatar.ID,
		"user_id":           avatar.UserID,
		"file_name":         avatar.FileName,
		"mime_type":         avatar.MimeType,
		"size_bytes":        avatar.SizeBytes,
		"s3_key":            avatar.S3Key,
		"thumbnail_s3_keys": avatar.ThumbnailS3Keys,
		"upload_status":     avatar.UploadStatus,
		"processing_status": avatar.ProcessingStatus,
		"created_at":        avatar.CreatedAt,
		"updated_at":        avatar.UpdatedAt,
	}

	_, err := r.db.NamedExecContext(ctx, query, args)
	return err
}

func (r *avatarRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	avatar := &domain.Avatar{}
	query := `SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		thumbnail_s3_keys, upload_status, processing_status, created_at, updated_at, deleted_at
		FROM avatars WHERE id = $1 AND deleted_at IS NULL`

	err := r.db.GetContext(ctx, avatar, query, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		return nil, err
	}
	return avatar, nil
}

func (r *avatarRepo) GetDeletedInfo(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	avatar := &domain.Avatar{}
	query := `SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		thumbnail_s3_keys, upload_status, processing_status, created_at, updated_at, deleted_at
		FROM avatars WHERE id = $1`

	err := r.db.GetContext(ctx, avatar, query, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		return nil, err
	}
	return avatar, nil
}

func (r *avatarRepo) GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error) {
	avatar := &domain.Avatar{}
	query := `SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		thumbnail_s3_keys, upload_status, processing_status, created_at, updated_at, deleted_at
		FROM avatars WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`

	err := r.db.GetContext(ctx, avatar, query, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
		}
		return nil, err
	}
	return avatar, nil
}

func (r *avatarRepo) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var avatars []*domain.Avatar
	query := `SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		thumbnail_s3_keys, upload_status, processing_status, created_at, updated_at, deleted_at
		FROM avatars WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`

	err := r.db.SelectContext(ctx, &avatars, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	return avatars, nil
}

func (r *avatarRepo) UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE avatars SET processing_status = $1 WHERE id = $2 AND deleted_at IS NULL`
	result, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *avatarRepo) UpdateUploadStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE avatars SET upload_status = $1 WHERE id = $2 AND deleted_at IS NULL`
	result, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *avatarRepo) UpdateThumbnailKeys(ctx context.Context, id uuid.UUID, keys map[string]string) error {
	query := `UPDATE avatars SET thumbnail_s3_keys = $1, processing_status = $2 WHERE id = $3 AND deleted_at IS NULL`
	result, err := r.db.ExecContext(ctx, query, domain.JSONMap(keys), domain.ProcessingComplete, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *avatarRepo) SoftDelete(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE avatars SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *avatarRepo) SoftDeleteByUserID(ctx context.Context, userID string) error {
	query := `UPDATE avatars SET deleted_at = NOW() WHERE id IN (SELECT id FROM avatars WHERE user_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1)`
	result, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *avatarRepo) SoftDeleteOwned(ctx context.Context, id uuid.UUID, userID string) error {
	query := `UPDATE avatars SET deleted_at = NOW() WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`
	result, err := r.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *avatarRepo) SoftDeleteLatestOwnedByUserID(ctx context.Context, userID string) error {
	query := `UPDATE avatars SET deleted_at = NOW() WHERE id IN (SELECT id FROM avatars WHERE user_id = $1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 1)`
	result, err := r.db.ExecContext(ctx, query, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
