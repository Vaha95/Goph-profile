package services

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// Sentinel errors so callers can classify failures without parsing error text.
var (
	ErrInvalidFormat = errors.New("invalid file format")
	ErrFileTooLarge  = errors.New("file too large")
)

var allowedMIMETypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
}

type MIMEValidator interface {
	Validate(reader io.Reader, maxSize int64) (mimeType string, size int64, err error)
}

type mimeValidator struct{}

func NewMIMEValidator() MIMEValidator {
	return &mimeValidator{}
}

func (v *mimeValidator) Validate(reader io.Reader, maxSize int64) (string, int64, error) {
	buf := make([]byte, 512)
	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		return "", 0, fmt.Errorf("read file header: %w", err)
	}
	if n == 0 {
		return "", 0, errors.New("empty file")
	}

	detectedType := detectMagicBytes(buf[:n])
	if detectedType == "" || !allowedMIMETypes[detectedType] {
		return "", 0, fmt.Errorf("%w: supported formats are jpeg, png, webp", ErrInvalidFormat)
	}

	// Count the remaining bytes without buffering them in memory, so the
	// caller can stream the file directly to S3 afterwards.
	counted, err := io.Copy(io.Discard, reader)
	if err != nil {
		return "", 0, fmt.Errorf("read file: %w", err)
	}

	totalSize := int64(n) + counted
	if totalSize > maxSize {
		return "", 0, fmt.Errorf("%w: %d bytes (max %d)", ErrFileTooLarge, totalSize, maxSize)
	}

	return detectedType, totalSize, nil
}

func detectMagicBytes(buf []byte) string {
	switch {
	case bytes.HasPrefix(buf, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case bytes.HasPrefix(buf, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		return "image/png"
	case len(buf) >= 12 && bytes.Equal(buf[:4], []byte("RIFF")) && bytes.Equal(buf[8:12], []byte("WEBP")):
		return "image/webp"
	}
	return ""
}
