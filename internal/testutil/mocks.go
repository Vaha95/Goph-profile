package testutil

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"time"
)

// MockAvatarRepository implements repository.AvatarRepository for unit tests.
type MockAvatarRepository struct {
	CreateFunc                        func(ctx context.Context, avatar *domain.Avatar) error
	GetByIDFunc                       func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error)
	GetLatestByUserIDFunc             func(ctx context.Context, userID string) (*domain.Avatar, error)
	ListByUserIDFunc                  func(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error)
	UpdateProcessingFunc              func(ctx context.Context, id uuid.UUID, status string) error
	UpdateUploadFunc                  func(ctx context.Context, id uuid.UUID, status string) error
	UpdateThumbnailFunc               func(ctx context.Context, id uuid.UUID, keys map[string]string) error
	SoftDeleteFunc                    func(ctx context.Context, id uuid.UUID) error
	SoftDeleteByUserFunc              func(ctx context.Context, userID string) error
	SoftDeleteOwnedFunc               func(ctx context.Context, id uuid.UUID, userID string) error
	SoftDeleteLatestOwnedByUserIDFunc func(ctx context.Context, userID string) error
	GetDeletedInfoFunc                func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error)
}

func (m *MockAvatarRepository) Create(ctx context.Context, avatar *domain.Avatar) error {
	return m.CreateFunc(ctx, avatar)
}
func (m *MockAvatarRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	return m.GetByIDFunc(ctx, id)
}
func (m *MockAvatarRepository) GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error) {
	return m.GetLatestByUserIDFunc(ctx, userID)
}
func (m *MockAvatarRepository) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
	return m.ListByUserIDFunc(ctx, userID, limit, offset)
}
func (m *MockAvatarRepository) UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status string) error {
	return m.UpdateProcessingFunc(ctx, id, status)
}
func (m *MockAvatarRepository) UpdateUploadStatus(ctx context.Context, id uuid.UUID, status string) error {
	return m.UpdateUploadFunc(ctx, id, status)
}
func (m *MockAvatarRepository) UpdateThumbnailKeys(ctx context.Context, id uuid.UUID, keys map[string]string) error {
	return m.UpdateThumbnailFunc(ctx, id, keys)
}
func (m *MockAvatarRepository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	return m.SoftDeleteFunc(ctx, id)
}
func (m *MockAvatarRepository) SoftDeleteByUserID(ctx context.Context, userID string) error {
	return m.SoftDeleteByUserFunc(ctx, userID)
}
func (m *MockAvatarRepository) SoftDeleteLatestOwnedByUserID(ctx context.Context, userID string) error {
	return m.SoftDeleteLatestOwnedByUserIDFunc(ctx, userID)
}
func (m *MockAvatarRepository) SoftDeleteOwned(ctx context.Context, id uuid.UUID, userID string) error {
	return m.SoftDeleteOwnedFunc(ctx, id, userID)
}
func (m *MockAvatarRepository) GetDeletedInfo(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
	return m.GetDeletedInfoFunc(ctx, id)
}

// MockS3Service implements services.S3Service for unit tests.
type MockS3Service struct {
	UploadFunc       func(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	DownloadFunc     func(ctx context.Context, key string) (io.ReadCloser, error)
	DeleteFunc       func(ctx context.Context, keys []string) error
	BucketExistsFunc func(ctx context.Context) (bool, error)
	PresignGetFunc   func(ctx context.Context, key string, expiresIn time.Duration) (string, error)
}

func (m *MockS3Service) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	return m.UploadFunc(ctx, key, reader, size, contentType)
}
func (m *MockS3Service) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	return m.DownloadFunc(ctx, key)
}
func (m *MockS3Service) Delete(ctx context.Context, keys []string) error {
	return m.DeleteFunc(ctx, keys)
}
func (m *MockS3Service) BucketExists(ctx context.Context) (bool, error) {
	return m.BucketExistsFunc(ctx)
}
func (m *MockS3Service) PresignGetURL(ctx context.Context, key string, expiresIn time.Duration) (string, error) {
	return m.PresignGetFunc(ctx, key, expiresIn)
}

// MockRabbitMQPublisher implements services.RabbitMQPublisher for unit tests.
type MockRabbitMQPublisher struct {
	PublishUploadFunc func(ctx context.Context, event map[string]any) error
	CloseFunc         func() error
	IsConnectedFunc   func() bool
}

func (m *MockRabbitMQPublisher) PublishUploadEvent(ctx context.Context, event map[string]any) error {
	return m.PublishUploadFunc(ctx, event)
}
func (m *MockRabbitMQPublisher) Close() error {
	return m.CloseFunc()
}
func (m *MockRabbitMQPublisher) IsConnected() bool {
	return m.IsConnectedFunc()
}

// MockMIMEValidator implements services.MIMEValidator for unit tests.
type MockMIMEValidator struct {
	ValidateFunc func(reader io.Reader, maxSize int64) (mimeType string, size int64, err error)
}

func (m *MockMIMEValidator) Validate(reader io.Reader, maxSize int64) (string, int64, error) {
	return m.ValidateFunc(reader, maxSize)
}
