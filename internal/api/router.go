package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gophprofile/avatars-service/internal/config"
	"github.com/gophprofile/avatars-service/internal/handlers"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
)

type Router struct {
	echo *echo.Echo
}

func NewRouter(
	cfg *config.Config,
	db *sql.DB,
	svc *services.AvatarService,
	rmq services.RabbitMQPublisher,
	s3 services.S3Service,
) *Router {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Unified error format for all routes (middleware, handlers, body limit, etc.):
	// {"error": "..."} instead of echo's default {"message": "..."}.
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		code := http.StatusInternalServerError
		message := "Internal server error"
		var he *echo.HTTPError
		if errors.As(err, &he) {
			code = he.Code
			if msg, ok := he.Message.(string); ok {
				message = msg
			}
		}
		_ = c.JSON(code, map[string]string{"error": message})
	}

	e.Use(echoMiddleware.Recover())
	e.Use(echoMiddleware.Logger())
	e.Use(CORSConfig(cfg.CORSAllowedOrigins))
	e.Use(NewRateLimiter(200))

	avatarHandler := handlers.NewAvatarHandler(svc)
	userHandler := handlers.NewUserHandler(svc)
	healthHandler := handlers.NewHealthHandler(db, rmq, s3)
	webHandler := handlers.NewWebHandler("web")

	e.GET("/health", healthHandler.Check)

	web := e.Group("/web")
	web.GET("/upload", webHandler.UploadPage)
	web.GET("/gallery/:user_id", webHandler.GalleryPage)

	e.Static("/static", "web/static")

	v1 := e.Group("/api/v1")

	avatars := v1.Group("/avatars")
	avatars.POST("", avatarHandler.Upload, RequireUserID, BodyLimit(cfg.MaxUploadSize))
	avatars.GET("/:avatar_id", avatarHandler.Get)
	avatars.DELETE("/:avatar_id", avatarHandler.Delete, RequireUserID)
	avatars.GET("/:avatar_id/metadata", avatarHandler.GetMetadata)

	users := v1.Group("/users")
	users.GET("/:user_id/avatar", userHandler.GetAvatar)
	users.DELETE("/:user_id/avatar", userHandler.DeleteAvatar, RequireUserID)
	users.GET("/:user_id/avatars", avatarHandler.ListByUser)

	return &Router{echo: e}
}

func (r *Router) Start(cfg *config.Config) error {
	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	log.Printf("server starting on %s", addr)

	r.echo.Server.Addr = addr
	r.echo.Server.ReadTimeout = 30 * time.Second
	r.echo.Server.ReadHeaderTimeout = 10 * time.Second
	r.echo.Server.WriteTimeout = 30 * time.Second
	r.echo.Server.IdleTimeout = 60 * time.Second

	return r.echo.StartServer(r.echo.Server)
}

func (r *Router) Shutdown(ctx context.Context) error {
	return r.echo.Shutdown(ctx)
}

func (r *Router) Echo() *echo.Echo {
	return r.echo
}
