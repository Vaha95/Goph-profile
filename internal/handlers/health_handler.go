package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/labstack/echo/v4"
)

type HealthHandler struct {
	db  *sql.DB
	rmq services.RabbitMQPublisher
	s3  services.S3Service
}

func NewHealthHandler(db *sql.DB, rmq services.RabbitMQPublisher, s3 services.S3Service) *HealthHandler {
	return &HealthHandler{db: db, rmq: rmq, s3: s3}
}

func (h *HealthHandler) Check(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 5*time.Second)
	defer cancel()

	type result struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	var wg sync.WaitGroup
	results := make(chan result, 3)

	wg.Add(3)

	go func() {
		defer wg.Done()
		err := h.db.PingContext(ctx)
		if err != nil {
			results <- result{Name: "database", Status: "error"}
			return
		}
		results <- result{Name: "database", Status: "ok"}
	}()

	go func() {
		defer wg.Done()
		if rmq, ok := h.rmq.(interface{ IsConnected() bool }); ok {
			if rmq.IsConnected() {
				results <- result{Name: "rabbitmq", Status: "ok"}
			} else {
				results <- result{Name: "rabbitmq", Status: "error"}
			}
		} else {
			results <- result{Name: "rabbitmq", Status: "ok"}
		}
	}()

	go func() {
		defer wg.Done()
		_, err := h.s3.BucketExists(ctx)
		if err != nil {
			results <- result{Name: "s3", Status: "error"}
			return
		}
		results <- result{Name: "s3", Status: "ok"}
	}()

	wg.Wait()
	close(results)

	components := make(map[string]string)
	allOK := true
	for r := range results {
		components[r.Name] = r.Status
		if r.Status != "ok" {
			allOK = false
		}
	}

	resp := map[string]any{
		"status":     "ok",
		"components": components,
	}

	statusCode := http.StatusOK
	if !allOK {
		resp["status"] = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	return c.JSON(statusCode, resp)
}
