package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/gophprofile/avatars-service/internal/repository"
	"github.com/gophprofile/avatars-service/internal/services"
	"github.com/labstack/echo/v4"
)

type AvatarHandler struct {
	svc *services.AvatarService
}

func NewAvatarHandler(svc *services.AvatarService) *AvatarHandler {
	return &AvatarHandler{svc: svc}
}

type uploadResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

func (h *AvatarHandler) Upload(c echo.Context) error {
	userID, ok := c.Request().Context().Value(domain.UserIDCtxKey).(string)
	if !ok {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "Missing user ID"})
	}

	file, header, err := c.Request().FormFile("file")
	if err != nil {
		if err == http.ErrMissingFile {
			return c.JSON(http.StatusBadRequest, errorResponse{
				Error:   "Missing file",
				Details: "File is required",
			})
		}
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid file"})
	}
	defer file.Close()

	avatar, err := h.svc.UploadAvatar(c.Request().Context(), userID, file, header)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrFileTooLarge):
			return c.JSON(http.StatusRequestEntityTooLarge, errorResponse{
				Error:   "File too large",
				Details: err.Error(),
			})
		case errors.Is(err, services.ErrInvalidFormat):
			return c.JSON(http.StatusBadRequest, errorResponse{
				Error:   "Invalid file format",
				Details: "Supported formats: jpeg, png, webp",
			})
		default:
			return c.JSON(http.StatusInternalServerError, errorResponse{Error: "Upload failed"})
		}
	}

	return c.JSON(http.StatusCreated, uploadResponse{
		ID:        avatar.ID.String(),
		UserID:    avatar.UserID,
		URL:       fmt.Sprintf("/api/v1/avatars/%s", avatar.ID),
		Status:    avatar.ProcessingStatus,
		CreatedAt: avatar.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *AvatarHandler) Get(c echo.Context) error {
	avatarID := c.Param("avatar_id")
	id, err := uuid.Parse(avatarID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid avatar ID"})
	}

	size := c.QueryParam("size")

	if size != "" && size != "original" && size != "100x100" && size != "300x300" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid size parameter"})
	}

	opts := services.ImageOptions{Size: size}
	reader, contentType, err := h.svc.GetAvatarImage(c.Request().Context(), id, opts)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return c.JSON(http.StatusNotFound, errorResponse{Error: "Avatar not found"})
		}
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "Failed to get avatar"})
	}
	defer reader.Close()

	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Cache-Control", "max-age=86400")

	etagSize := size
	if etagSize == "" {
		etagSize = "original"
	}
	c.Response().Header().Set("ETag", fmt.Sprintf("%q", id.String()+"-"+etagSize))
	return c.Stream(http.StatusOK, contentType, reader)
}

func (h *AvatarHandler) Delete(c echo.Context) error {
	avatarID := c.Param("avatar_id")
	id, err := uuid.Parse(avatarID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid avatar ID"})
	}

	ctx := c.Request().Context()
	requestUserID, _ := ctx.Value(domain.UserIDCtxKey).(string)

	err = h.svc.DeleteAvatar(ctx, id, requestUserID)
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

func (h *AvatarHandler) GetMetadata(c echo.Context) error {
	avatarID := c.Param("avatar_id")
	id, err := uuid.Parse(avatarID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "Invalid avatar ID"})
	}

	avatar, err := h.svc.GetAvatar(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, errorResponse{Error: "Avatar not found"})
	}

	type thumbnailInfo struct {
		Size string `json:"size"`
		URL  string `json:"url"`
	}

	var thumbnails []thumbnailInfo
	for size := range avatar.ThumbnailS3Keys {
		thumbnails = append(thumbnails, thumbnailInfo{
			Size: size,
			URL:  fmt.Sprintf("/api/v1/avatars/%s?size=%s", avatar.ID, size),
		})
	}

	resp := map[string]any{
		"id":                avatar.ID.String(),
		"user_id":           avatar.UserID,
		"file_name":         avatar.FileName,
		"mime_type":         avatar.MimeType,
		"size":              avatar.SizeBytes,
		"thumbnails":        thumbnails,
		"created_at":        avatar.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":        avatar.UpdatedAt.UTC().Format(time.RFC3339),
		"processing_status": avatar.ProcessingStatus,
	}

	return c.JSON(http.StatusOK, resp)
}

func (h *AvatarHandler) ListByUser(c echo.Context) error {
	userID := c.Param("user_id")

	limitStr := c.QueryParam("limit")
	offsetStr := c.QueryParam("offset")

	limit := 20
	offset := 0

	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
			if limit > 100 {
				limit = 100
			}
		}
	}
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	avatars, err := h.svc.ListAvatarsByUser(c.Request().Context(), userID, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "Failed to list avatars"})
	}

	type avatarListItem struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Status  string `json:"status"`
		Created string `json:"created_at"`
	}

	items := make([]avatarListItem, len(avatars))
	for i, a := range avatars {
		items[i] = avatarListItem{
			ID:      a.ID.String(),
			URL:     fmt.Sprintf("/api/v1/avatars/%s", a.ID),
			Status:  a.ProcessingStatus,
			Created: a.CreatedAt.UTC().Format(time.RFC3339),
		}
	}

	return c.JSON(http.StatusOK, items)
}
