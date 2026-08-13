package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v4"
)

type WebHandler struct {
	webRoot string
}

func NewWebHandler(webRoot string) *WebHandler {
	if webRoot == "" {
		webRoot = "web"
	}
	return &WebHandler{webRoot: webRoot}
}

func (h *WebHandler) UploadPage(c echo.Context) error {
	return h.serveFile("upload.html", c)
}

func (h *WebHandler) GalleryPage(c echo.Context) error {
	userID := c.Param("user_id")
	if userID == "" {
		return c.Redirect(http.StatusFound, "/web/upload")
	}
	return h.serveFile("gallery.html", c)
}

func (h *WebHandler) serveFile(filename string, c echo.Context) error {
	path := filepath.Join(h.webRoot, filename)
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return c.NoContent(http.StatusNotFound)
	}

	return c.File(path)
}
