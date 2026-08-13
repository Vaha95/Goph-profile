package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func RequireUserID(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		userID := c.Request().Header.Get("X-User-ID")
		if userID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "X-User-ID header is required")
		}
		userID = strings.TrimSpace(userID)
		if len(userID) > 255 {
			return echo.NewHTTPError(http.StatusBadRequest, "X-User-ID too long")
		}
		if !isValidUserID(userID) {
			return echo.NewHTTPError(http.StatusBadRequest, "X-User-ID must be alphanumeric with dashes/underscores")
		}

		ctx := context.WithValue(c.Request().Context(), domain.UserIDCtxKey, userID)
		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func BodyLimit(maxSize int64) echo.MiddlewareFunc {
	return middleware.BodyLimit(fmt.Sprintf("%d", maxSize))
}

func CORSConfig(origins string) echo.MiddlewareFunc {
	return middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     strings.Split(origins, ","),
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderContentType, "X-User-ID"},
		ExposeHeaders:    []string{echo.HeaderContentType},
		MaxAge:           86400,
		AllowCredentials: false,
	})
}

func NewRateLimiter(maxRequestsPerMin int) echo.MiddlewareFunc {
	rl := newTokenBucket(maxRequestsPerMin)
	return rl.limit
}

type tokenBucket struct {
	mu        sync.Mutex
	clients   map[string]*clientBucket
	maxPerMin int
}

type clientBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
}

func newTokenBucket(maxPerMin int) *tokenBucket {
	tb := &tokenBucket{
		clients:   make(map[string]*clientBucket),
		maxPerMin: maxPerMin,
	}

	go tb.cleanupLoop()
	return tb
}

func (tb *tokenBucket) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		tb.mu.Lock()
		now := time.Now()
		for ip, cb := range tb.clients {
			if now.Sub(cb.lastRefill) > 10*time.Minute {
				delete(tb.clients, ip)
			}
		}
		tb.mu.Unlock()
	}
}

func (tb *tokenBucket) limit(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ip := extractIP(c.Request())
		if ip == "" {
			return next(c)
		}

		tb.mu.Lock()
		cb, ok := tb.clients[ip]
		if !ok {
			cb = &clientBucket{
				tokens:     float64(tb.maxPerMin) - 1,
				maxTokens:  float64(tb.maxPerMin),
				refillRate: float64(tb.maxPerMin) / 60.0,
				lastRefill: time.Now(),
			}
			tb.clients[ip] = cb
		} else {
			now := time.Now()
			elapsed := now.Sub(cb.lastRefill).Seconds()
			cb.tokens += elapsed * cb.refillRate
			if cb.tokens > cb.maxTokens {
				cb.tokens = cb.maxTokens
			}
			cb.lastRefill = now
		}

		cb.tokens--
		if cb.tokens < 0 {
			tb.mu.Unlock()
			return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
		}
		tb.mu.Unlock()

		return next(c)
	}
}

func extractIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func isValidUserID(id string) bool {
	for _, r := range id {
		if !isValidUserIDChar(r) {
			return false
		}
	}
	return true
}

func isValidUserIDChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}
