package testutil

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
)

// NewTestAvatar creates a test avatar with a valid UUID.
func NewTestAvatar() *domain.Avatar {
	now := time.Now().UTC()
	id := uuid.New()
	return &domain.Avatar{
		ID:               id,
		UserID:           "user_123",
		FileName:         "photo.jpg",
		MimeType:         "image/jpeg",
		SizeBytes:        1024,
		S3Key:            "user_123/avatar.jpg",
		ThumbnailS3Keys:  domain.JSONMap{"100x100": "user_123/" + id.String() + "/100x100.jpg", "300x300": "user_123/" + id.String() + "/300x300.jpg"},
		UploadStatus:     domain.StatusUploaded,
		ProcessingStatus: domain.ProcessingComplete,
		CreatedAt:        now,
		UpdatedAt:        now,
		DeletedAt:        nil,
	}
}

// JPEGBytes returns a minimal valid JPEG image.
func JPEGBytes() []byte {
	buf := new(bytes.Buffer)
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	jpeg.Encode(buf, img, &jpeg.Options{Quality: 90})
	return buf.Bytes()
}

// PNGBytes returns a minimal valid PNG image.
func PNGBytes() []byte {
	buf := new(bytes.Buffer)
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 50; x++ {
			img.Set(x, y, color.RGBA{R: 0, G: 255, B: 0, A: 255})
		}
	}
	png.Encode(buf, img)
	return buf.Bytes()
}

// JPEGFileHeaderBytes returns JPEG magic bytes for MIME detection.
func JPEGFileHeaderBytes() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0}
}

// PNGFileHeaderBytes returns PNG magic bytes for MIME detection.
func PNGFileHeaderBytes() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
}

// WebPFileHeaderBytes returns WebP magic bytes for MIME detection.
func WebPFileHeaderBytes() []byte {
	return []byte("RIFF\x00\x00\x00\x00WEBP")
}
