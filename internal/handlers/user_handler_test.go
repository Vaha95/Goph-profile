package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/labstack/echo/v4"
)

func TestUserHandler_GetAvatar(t *testing.T) {
	e := newTestEcho()
	h := NewUserHandler(newMockService())
	e.GET("/users/:user_id/avatar", h.GetAvatar)

	req := httptest.NewRequest(http.MethodGet, "/users/user_1/avatar", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Should have cache-control header
	if rec.Header().Get("Cache-Control") == "" {
		t.Error("expected Cache-Control header")
	}
}

func TestUserHandler_DeleteAvatar_OwnAvatar(t *testing.T) {
	e := newTestEcho()
	h := NewUserHandler(newMockService())

	e.DELETE("/users/:user_id/avatar", h.DeleteAvatar, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, "user_123")
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	req := httptest.NewRequest(http.MethodDelete, "/users/user_123/avatar", nil)
	req.Header.Set("X-User-ID", "user_123")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUserHandler_DeleteAvatar_Forbidden(t *testing.T) {
	e := newTestEcho()
	h := NewUserHandler(newMockService())

	e.DELETE("/users/:user_id/avatar", h.DeleteAvatar, func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, "attacker")
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	})

	req := httptest.NewRequest(http.MethodDelete, "/users/user_1/avatar", nil)
	req.Header.Set("X-User-ID", "attacker")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
