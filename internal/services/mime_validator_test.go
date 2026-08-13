package services

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/gophprofile/avatars-service/internal/testutil"
)

func TestMIMEValidator_Validate_JPEG(t *testing.T) {
	v := NewMIMEValidator()
	mime, size, err := v.Validate(bytes.NewReader(testutil.JPEGBytes()), 1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", mime)
	}
	if size <= 0 {
		t.Errorf("expected positive size, got %d", size)
	}
}

func TestMIMEValidator_Validate_PNG(t *testing.T) {
	v := NewMIMEValidator()
	mime, _, err := v.Validate(bytes.NewReader(testutil.PNGBytes()), 1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("expected image/png, got %s", mime)
	}
}

func TestMIMEValidator_Validate_WebP(t *testing.T) {
	v := NewMIMEValidator()
	// WebP RIFF header + dummy body
	webpHeader := testutil.WebPFileHeaderBytes()
	body := append(webpHeader, make([]byte, 64)...)
	mime, _, err := v.Validate(bytes.NewReader(body), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/webp" {
		t.Errorf("expected image/webp, got %s", mime)
	}
}

func TestMIMEValidator_Validate_EmptyFile(t *testing.T) {
	v := NewMIMEValidator()
	_, _, err := v.Validate(bytes.NewReader([]byte{}), 1024)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !errors.Is(err, io.EOF) && err.Error() == "" {
		t.Errorf("expected meaningful error, got: %v", err)
	}
}

func TestMIMEValidator_Validate_InvalidFormat(t *testing.T) {
	v := NewMIMEValidator()
	_, _, err := v.Validate(bytes.NewReader([]byte("NOT_AN_IMAGE")), 1024)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestMIMEValidator_Validate_FileTooLarge(t *testing.T) {
	v := NewMIMEValidator()
	// JPEG data that exceeds the maxSize limit
	data := testutil.JPEGBytes()
	_, _, err := v.Validate(bytes.NewReader(data), int64(len(data)-1))
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}

func TestMIMEValidator_Validate_ReadError(t *testing.T) {
	v := NewMIMEValidator()
	_, _, err := v.Validate(errReader{}, 1024)
	if err == nil {
		t.Fatal("expected error for read failure")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
