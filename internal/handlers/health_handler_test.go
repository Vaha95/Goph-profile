package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophprofile/avatars-service/internal/testutil"
	"github.com/labstack/echo/v4"
	"time"
)

type mockConnector struct {
	pingErr error
}

func (c *mockConnector) Connect(ctx context.Context) (driver.Conn, error) {
	return &mockDBConn{pingErr: c.pingErr}, nil
}

func (c *mockConnector) Driver() driver.Driver {
	return nil
}

type mockDBConn struct {
	pingErr error
}

func (c *mockDBConn) Close() error                              { return nil }
func (c *mockDBConn) Begin() (driver.Tx, error)                 { return nil, nil }
func (c *mockDBConn) Prepare(query string) (driver.Stmt, error) { return nil, nil }
func (c *mockDBConn) Ping(ctx context.Context) error            { return c.pingErr }

type mockDBStmt struct{}

func (s *mockDBStmt) Close() error                                    { return nil }
func (s *mockDBStmt) NumInput() int                                   { return -1 }
func (s *mockDBStmt) Exec(args []driver.Value) (driver.Result, error) { return nil, nil }
func (s *mockDBStmt) Query(args []driver.Value) (driver.Rows, error)  { return nil, nil }

type mockDBRows struct{}

func (r *mockDBRows) Close() error                   { return nil }
func (r *mockDBRows) Columns() []string              { return nil }
func (r *mockDBRows) Next(dest []driver.Value) error { return io.EOF }

func TestHealthHandler_Degraded_NoDB(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	db := sql.OpenDB(&mockConnector{pingErr: errors.New("not connected")})

	mockRMQ := &testutil.MockRabbitMQPublisher{
		IsConnectedFunc: func() bool { return false },
	}
	mockS3 := &testutil.MockS3Service{
		BucketExistsFunc: func(ctx context.Context) (bool, error) { return false, errors.New("refused") },
		PresignGetFunc:   func(ctx context.Context, key string, expiresIn time.Duration) (string, error) { return "", nil },
	}

	h := NewHealthHandler(db, mockRMQ, mockS3)
	e.GET("/health", h.Check)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHealthHandler_DBOK(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	db := sql.OpenDB(&mockConnector{pingErr: nil})

	mockRMQ := &testutil.MockRabbitMQPublisher{
		IsConnectedFunc: func() bool { return true },
	}
	mockS3 := &testutil.MockS3Service{
		BucketExistsFunc: func(ctx context.Context) (bool, error) { return true, nil },
		PresignGetFunc:   func(ctx context.Context, key string, expiresIn time.Duration) (string, error) { return "", nil },
	}

	h := NewHealthHandler(db, mockRMQ, mockS3)
	e.GET("/health", h.Check)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
