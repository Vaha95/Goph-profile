package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/repository"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/labstack/echo/v4"
)

type UserHandler struct {
	svc *services.AvatarService
}

func NewUserHandler(svc *services.AvatarService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) GetAvatar(c echo.Context) error {
	userID := c.Param("user_id")
	size := c.QueryParam("size")

	if size != "" && size != "original" && size != "100x100" && size != "300x300" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid size parameter"})
	}

	avatar, err := h.svc.GetLatestAvatarByUser(c.Request().Context(), userID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return c.JSON(http.StatusNotFound, errorResponse{Error: "No avatar found for user"})
		}
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "Failed to get avatar"})
	}

	opts := services.ImageOptions{Size: size}
	reader, contentType, err := h.svc.GetAvatarImage(c.Request().Context(), avatar.ID, opts)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return c.JSON(http.StatusNotFound, errorResponse{Error: "No avatar found for user"})
		}
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "Failed to get avatar"})
	}
	defer reader.Close()

	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Cache-Control", "max-age=86400")
	c.Response().Header().Set("ETag", fmt.Sprintf("%q", avatar.ID.String()))
	return c.Stream(http.StatusOK, contentType, reader)
}

func (h *UserHandler) DeleteAvatar(c echo.Context) error {
	userID := c.Param("user_id")

	requestUserID := ""
	if uid, ok := c.Request().Context().Value(domain.UserIDCtxKey).(string); ok {
		requestUserID = uid
	}
	if requestUserID != "" && requestUserID != userID {
		return c.JSON(http.StatusForbidden, errorResponse{
			Error:   "Forbidden",
			Details: "You can only delete your own avatars",
		})
	}

	err := h.svc.DeleteLatestAvatarByUser(c.Request().Context(), userID, requestUserID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrForbidden):
			return c.JSON(http.StatusForbidden, errorResponse{
				Error:   "Forbidden",
				Details: "You can only delete your own avatars",
			})
		case errors.Is(err, repository.ErrNotFound):
			return c.JSON(http.StatusNotFound, errorResponse{Error: "Avatar not found"})
		default:
			return c.JSON(http.StatusInternalServerError, errorResponse{Error: "Delete failed"})
		}
	}

	return c.NoContent(http.StatusNoContent)
}
