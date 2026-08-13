package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/gophprofile/avatars-service/internal/testutil"
	"github.com/labstack/echo/v4"
)

// --- Non-image file with spoofed Content-Type ---

func TestAvatarHandler_Upload_SpoofedContentType(t *testing.T) {
	mockS3 := &testutil.MockS3Service{
		BucketExistsFunc: func(ctx context.Context) (bool, error) { return true, nil },
		PresignGetFunc:   func(ctx context.Context, key string, expiresIn time.Duration) (string, error) { return "", nil },
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		PublishUploadFunc: func(ctx context.Context, event map[string]any) error { return nil },
		CloseFunc:         func() error { return nil },
	}
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) {
			return "", 0, fmt.Errorf("file format not supported: %w", services.ErrInvalidFormat)
		},
	}
	mockRepo := &testutil.MockAvatarRepository{}
	svc := services.NewAvatarService(mockRepo, mockS3, mockRMQ, mockMime, 10*1024*1024)

	e := newTestEcho()
	h := NewAvatarHandler(svc)

	e.POST("/upload", h.Upload, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, "user_1")
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	fw, _ := w.CreateFormFile("file", "evil.exe")
	fw.Write([]byte("MZ\x90\x00"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = req.WithContext(context.WithValue(req.Context(), domain.UserIDCtxKey, "user_1"))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- Ownership check in AvatarHandler.Delete ---

func TestAvatarHandler_Delete_OwnAvatar(t *testing.T) {
	avatar := testutil.NewTestAvatar()

	mockRepo := &testutil.MockAvatarRepository{
		GetDeletedInfoFunc:  func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) { return avatar, nil },
		SoftDeleteOwnedFunc: func(ctx context.Context, id uuid.UUID, userID string) error { return nil },
	}
	mockS3 := &testutil.MockS3Service{
		DeleteFunc:       func(ctx context.Context, keys []string) error { return nil },
		BucketExistsFunc: func(ctx context.Context) (bool, error) { return true, nil },
		PresignGetFunc:   func(ctx context.Context, key string, expiresIn time.Duration) (string, error) { return "", nil },
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		CloseFunc: func() error { return nil },
	}
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) { return "image/jpeg", 0, nil },
	}
	svc := services.NewAvatarService(mockRepo, mockS3, mockRMQ, mockMime, 1024)

	e := newTestEcho()
	h := NewAvatarHandler(svc)

	e.DELETE("/avatars/:avatar_id", h.Delete, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, avatar.UserID)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	req := httptest.NewRequest(http.MethodDelete, "/avatars/"+avatar.ID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), domain.UserIDCtxKey, avatar.UserID))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAvatarHandler_Delete_Forbidden(t *testing.T) {
	avatar := testutil.NewTestAvatar()

	mockRepo := &testutil.MockAvatarRepository{
		GetDeletedInfoFunc: func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) { return avatar, nil },
	}
	mockS3 := &testutil.MockS3Service{
		BucketExistsFunc: func(ctx context.Context) (bool, error) { return true, nil },
		PresignGetFunc:   func(ctx context.Context, key string, expiresIn time.Duration) (string, error) { return "", nil },
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		CloseFunc: func() error { return nil },
	}
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) { return "image/jpeg", 0, nil },
	}
	svc := services.NewAvatarService(mockRepo, mockS3, mockRMQ, mockMime, 1024)

	e := newTestEcho()
	h := NewAvatarHandler(svc)

	e.DELETE("/avatars/:avatar_id", h.Delete, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, "attacker")
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	req := httptest.NewRequest(http.MethodDelete, "/avatars/"+avatar.ID.String(), nil)
	req = req.WithContext(context.WithValue(req.Context(), domain.UserIDCtxKey, "attacker"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
