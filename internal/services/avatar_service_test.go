package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"testing"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/testutil"
)

func TestAvatarService_UploadAvatar_Success(t *testing.T) {
	ctx := context.Background()

	mockRepo := &testutil.MockAvatarRepository{
		CreateFunc: func(ctx context.Context, avatar *domain.Avatar) error { return nil },
	}
	mockS3 := &testutil.MockS3Service{
		UploadFunc: func(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
			return nil
		},
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		PublishUploadFunc: func(ctx context.Context, event map[string]any) error { return nil },
	}
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) {
			return "image/jpeg", 1024, nil
		},
	}

	svc := NewAvatarService(mockRepo, mockS3, mockRMQ, mockMime, 10*1024*1024)

	file := &dummyFile{data: testutil.JPEGBytes()}
	header := &multipart.FileHeader{Filename: "test.jpg"}

	avatar, err := svc.UploadAvatar(ctx, "user_1", file, header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if avatar.UserID != "user_1" {
		t.Errorf("expected userID user_1, got %s", avatar.UserID)
	}
	if avatar.ProcessingStatus != domain.ProcessingPending {
		t.Errorf("expected pending, got %s", avatar.ProcessingStatus)
	}
}

func TestAvatarService_UploadAvatar_InvalidMIME(t *testing.T) {
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) {
			return "", 0, errors.New("invalid file format")
		},
	}

	svc := NewAvatarService(nil, nil, nil, mockMime, 10*1024*1024)
	file := &dummyFile{data: testutil.JPEGBytes()}
	header := &multipart.FileHeader{Filename: "test.jpg"}

	_, err := svc.UploadAvatar(context.Background(), "user_1", file, header)
	if err == nil {
		t.Fatal("expected error for invalid MIME")
	}
}

func TestAvatarService_UploadAvatar_S3Fail(t *testing.T) {
	mockRepo := &testutil.MockAvatarRepository{
		CreateFunc: func(ctx context.Context, avatar *domain.Avatar) error { return nil },
	}
	mockS3 := &testutil.MockS3Service{
		UploadFunc: func(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
			return errors.New("s3 failed")
		},
		DeleteFunc: func(ctx context.Context, keys []string) error { return nil },
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		PublishUploadFunc: func(ctx context.Context, event map[string]any) error { return nil },
	}
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) {
			return "image/jpeg", 1024, nil
		},
	}

	svc := NewAvatarService(mockRepo, mockS3, mockRMQ, mockMime, 10*1024*1024)
	file := &dummyFile{data: testutil.JPEGBytes()}
	header := &multipart.FileHeader{Filename: "test.jpg"}

	_, err := svc.UploadAvatar(context.Background(), "user_1", file, header)
	if err == nil {
		t.Fatal("expected error for S3 upload failure")
	}
}

func TestAvatarService_GetAvatarImage(t *testing.T) {
	ctx := context.Background()
	avatar := testutil.NewTestAvatar()
	id := avatar.ID

	mockRepo := &testutil.MockAvatarRepository{
		GetByIDFunc: func(ctx context.Context, i uuid.UUID) (*domain.Avatar, error) {
			return avatar, nil
		},
	}

	var downloadedKey string
	mockS3 := &testutil.MockS3Service{
		DownloadFunc: func(ctx context.Context, key string) (io.ReadCloser, error) {
			downloadedKey = key
			return io.NopCloser(bytes.NewReader(testutil.JPEGBytes())), nil
		},
	}

	svc := NewAvatarService(mockRepo, mockS3, nil, nil, 0)

	reader, contentType, err := svc.GetAvatarImage(ctx, id, ImageOptions{Size: "100x100"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contentType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", contentType)
	}
	if downloadedKey != avatar.ThumbnailS3Keys["100x100"] {
		t.Errorf("expected thumbnail key %s, got %s", avatar.ThumbnailS3Keys["100x100"], downloadedKey)
	}
	reader.Close()
}

func TestAvatarService_GetAvatarImage_Original(t *testing.T) {
	ctx := context.Background()
	avatar := testutil.NewTestAvatar()

	mockRepo := &testutil.MockAvatarRepository{
		GetByIDFunc: func(ctx context.Context, i uuid.UUID) (*domain.Avatar, error) {
			return avatar, nil
		},
	}

	var downloadedKey string
	mockS3 := &testutil.MockS3Service{
		DownloadFunc: func(ctx context.Context, key string) (io.ReadCloser, error) {
			downloadedKey = key
			return io.NopCloser(bytes.NewReader(testutil.JPEGBytes())), nil
		},
	}

	svc := NewAvatarService(mockRepo, mockS3, nil, nil, 0)
	reader, _, err := svc.GetAvatarImage(ctx, avatar.ID, ImageOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if downloadedKey != avatar.S3Key {
		t.Errorf("expected original key %s, got %s", avatar.S3Key, downloadedKey)
	}
	reader.Close()
}

func TestAvatarService_DeleteAvatar(t *testing.T) {
	ctx := context.Background()
	avatar := testutil.NewTestAvatar()

	var deletedKeys []string
	mockS3 := &testutil.MockS3Service{
		DeleteFunc: func(ctx context.Context, keys []string) error {
			deletedKeys = keys
			return nil
		},
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		CloseFunc: func() error { return nil },
	}
	var deletedID uuid.UUID
	mockRepo := &testutil.MockAvatarRepository{
		GetDeletedInfoFunc: func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
			return avatar, nil
		},
		SoftDeleteFunc: func(ctx context.Context, id uuid.UUID) error {
			deletedID = id
			return nil
		},
	}

	svc := NewAvatarService(mockRepo, mockS3, mockRMQ, nil, 0)
	err := svc.DeleteAvatar(ctx, avatar.ID, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deletedID != avatar.ID {
		t.Errorf("expected avatar ID to be deleted, got %s", deletedID)
	}
	if len(deletedKeys) != 3 {
		t.Errorf("expected 3 keys deleted, got %d", len(deletedKeys))
	}
}

type dummyFile struct {
	data []byte
	off  int64
}

func (d *dummyFile) Read(p []byte) (int, error) {
	if d.off >= int64(len(d.data)) {
		return 0, io.EOF
	}
	n := copy(p, d.data[d.off:])
	d.off += int64(n)
	return n, nil
}

func (d *dummyFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(d.data)) {
		return 0, io.EOF
	}
	n := copy(p, d.data[off:])
	return n, nil
}

func (d *dummyFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case 0:
		d.off = offset
	case 1:
		d.off += offset
	}
	return d.off, nil
}

func (d *dummyFile) Close() error { return nil }
