package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/labstack/echo/v4"
)

func TestRequireUserID_Missing(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}, RequireUserID)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireUserID_InvalidChars(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}, RequireUserID)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-User-ID", "user@123!")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireUserID_TooLong(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}, RequireUserID)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-User-ID", strings.Repeat("a", 256))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireUserID_Valid(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	e.GET("/test", func(c echo.Context) error {
		uid, _ := c.Request().Context().Value(domain.UserIDCtxKey).(string)
		return c.String(http.StatusOK, uid)
	}, RequireUserID)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-User-ID", "user-123_test")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "user-123_test" {
		t.Errorf("expected user-123_test in context, got %s", rec.Body.String())
	}
}

func TestRequireUserID_WhitespaceTrimmed(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	e.GET("/test", func(c echo.Context) error {
		uid, _ := c.Request().Context().Value(domain.UserIDCtxKey).(string)
		return c.String(http.StatusOK, uid)
	}, RequireUserID)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-User-ID", "  user_1  ")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "user_1" {
		t.Errorf("expected trimmed user_1, got %s", rec.Body.String())
	}
}

func TestRateLimiter_AllowUnderLimit(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	limit := NewRateLimiter(100)
	e.Use(limit)

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimiter_BlockAfterExhaust(t *testing.T) {
	e := echo.New()
	e.HideBanner = true

	// Token bucket starts with maxPerMin-1 = 1 token, so first request passes, second is blocked.
	limit := NewRateLimiter(2)
	e.Use(limit)

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	// First request: consumes the 1 initial token → passes
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("request 1: expected 200, got %d", rec.Code)
	}

	// Second request: no tokens left → blocked
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("request 2: expected 429, got %d", rec.Code)
	}
}

func TestCORS_CrossOriginHeaders(t *testing.T) {
	e := echo.New()
	e.HideBanner = true
	e.Use(CORSConfig("http://example.com"))

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
		t.Errorf("expected specific origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_BlockUnexpectedOrigin(t *testing.T) {
	e := echo.New()
	e.HideBanner = true
	e.Use(CORSConfig("http://example.com"))

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no Access-Control-Allow-Origin, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestIsValidUserID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"user_123", true},
		{"user-123", true},
		{"abc123", true},
		{"User123", true},
		{"user@123", false},
		{"user 123", false},
		{"", true},
		{"user!123", false},
	}

	for _, tt := range tests {
		got := isValidUserID(tt.id)
		if got != tt.want {
			t.Errorf("isValidUserID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}
