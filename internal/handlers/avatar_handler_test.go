package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/gophprofile/avatars-service/internal/testutil"
	"github.com/labstack/echo/v4"
)

func newTestEcho() *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	return e
}

func newMockService() *services.AvatarService {
	mockRepo := &testutil.MockAvatarRepository{
		GetByIDFunc:           func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) { return testutil.NewTestAvatar(), nil },
		GetLatestByUserIDFunc: func(ctx context.Context, userID string) (*domain.Avatar, error) { return testutil.NewTestAvatar(), nil },
		ListByUserIDFunc: func(ctx context.Context, userID string, limit, offset int) ([]*domain.Avatar, error) {
			return []*domain.Avatar{testutil.NewTestAvatar()}, nil
		},
		CreateFunc:                        func(ctx context.Context, avatar *domain.Avatar) error { return nil },
		SoftDeleteFunc:                    func(ctx context.Context, id uuid.UUID) error { return nil },
		SoftDeleteOwnedFunc:               func(ctx context.Context, id uuid.UUID, userID string) error { return nil },
		SoftDeleteLatestOwnedByUserIDFunc: func(ctx context.Context, userID string) error { return nil },
		GetDeletedInfoFunc:                func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) { return testutil.NewTestAvatar(), nil },
		UpdateProcessingFunc:              func(ctx context.Context, id uuid.UUID, status string) error { return nil },
	}
	mockS3 := &testutil.MockS3Service{
		UploadFunc: func(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
			return nil
		},
		DownloadFunc: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(testutil.JPEGBytes())), nil
		},
		DeleteFunc:       func(ctx context.Context, keys []string) error { return nil },
		BucketExistsFunc: func(ctx context.Context) (bool, error) { return true, nil },
		PresignGetFunc:   func(ctx context.Context, key string, expiresIn time.Duration) (string, error) { return "", nil },
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		PublishUploadFunc: func(ctx context.Context, event map[string]any) error { return nil },
		CloseFunc:         func() error { return nil },
		IsConnectedFunc:   func() bool { return true },
	}
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) { return "image/jpeg", 1024, nil },
	}
	return services.NewAvatarService(mockRepo, mockS3, mockRMQ, mockMime, 10*1024*1024)
}

func buildMultipartBody() (string, []byte) {
	buf := new(bytes.Buffer)
	w := multipart.NewWriter(buf)
	fw, _ := w.CreateFormFile("file", "test.jpg")
	fw.Write(testutil.JPEGBytes())
	w.Close()
	return w.FormDataContentType(), buf.Bytes()
}

func TestAvatarHandler_Upload_Success(t *testing.T) {
	e := newTestEcho()
	h := NewAvatarHandler(newMockService())

	e.POST("/upload", h.Upload, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, "user_1")
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	ct, body := buildMultipartBody()
	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req = req.WithContext(context.WithValue(req.Context(), domain.UserIDCtxKey, "user_1"))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAvatarHandler_Upload_MissingFile(t *testing.T) {
	e := newTestEcho()
	h := NewAvatarHandler(newMockService())

	e.POST("/upload", h.Upload, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, "user_1")
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(""))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAvatarHandler_Get_InvalidID(t *testing.T) {
	e := newTestEcho()
	h := NewAvatarHandler(newMockService())
	e.GET("/avatars/:avatar_id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/avatars/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAvatarHandler_Get_InvalidSize(t *testing.T) {
	e := newTestEcho()
	h := NewAvatarHandler(newMockService())
	e.GET("/avatars/:avatar_id", h.Get)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/avatars/"+id.String()+"?size=999x999", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAvatarHandler_Delete(t *testing.T) {
	avatar := testutil.NewTestAvatar()

	mockRepo := &testutil.MockAvatarRepository{
		GetByIDFunc:        func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) { return avatar, nil },
		GetDeletedInfoFunc: func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) { return avatar, nil },
		SoftDeleteFunc:     func(ctx context.Context, id uuid.UUID) error { return nil },
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
	e.DELETE("/avatars/:avatar_id", h.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/avatars/"+avatar.ID.String(), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAvatarHandler_GetMetadata(t *testing.T) {
	e := newTestEcho()
	h := NewAvatarHandler(newMockService())
	e.GET("/avatars/:avatar_id/metadata", h.GetMetadata)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/avatars/"+id.String()+"/metadata", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAvatarHandler_GetMetadata_InvalidID(t *testing.T) {
	e := newTestEcho()
	h := NewAvatarHandler(newMockService())
	e.GET("/avatars/:avatar_id/metadata", h.GetMetadata)

	req := httptest.NewRequest(http.MethodGet, "/avatars/bad-id/metadata", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAvatarHandler_GetMetadata_NotFound(t *testing.T) {
	e := newTestEcho()
	mockRepo := &testutil.MockAvatarRepository{
		GetByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Avatar, error) {
			return nil, errors.New("not found")
		},
	}
	mockS3 := &testutil.MockS3Service{
		BucketExistsFunc: func(ctx context.Context) (bool, error) { return true, nil },
		PresignGetFunc:   func(ctx context.Context, key string, expiresIn time.Duration) (string, error) { return "", nil },
	}
	mockRMQ := &testutil.MockRabbitMQPublisher{
		PublishUploadFunc: func(ctx context.Context, event map[string]any) error { return nil },
		CloseFunc:         func() error { return nil },
	}
	mockMime := &testutil.MockMIMEValidator{
		ValidateFunc: func(reader io.Reader, maxSize int64) (string, int64, error) { return "image/jpeg", 0, nil },
	}
	svc := services.NewAvatarService(mockRepo, mockS3, mockRMQ, mockMime, 1024)

	h := NewAvatarHandler(svc)
	e.GET("/avatars/:avatar_id/metadata", h.GetMetadata)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/avatars/"+id.String()+"/metadata", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAvatarHandler_ListByUser(t *testing.T) {
	e := newTestEcho()
	h := NewAvatarHandler(newMockService())
	e.GET("/users/:user_id/avatars", h.ListByUser)

	req := httptest.NewRequest(http.MethodGet, "/users/user_1/avatars?limit=5&offset=0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
