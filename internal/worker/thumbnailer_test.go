package worker

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gophprofile/avatars-service/internal/domain"
)

func TestThumbnailer_Generate_Sizes(t *testing.T) {
	th := NewThumbnailer()

	buf := new(bytes.Buffer)
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	jpeg.Encode(buf, img, &jpeg.Options{Quality: 90})
	data := buf.Bytes()

	sizes := []domain.ThumbnailSize{
		{Width: 50, Height: 50},
		{Width: 100, Height: 100},
	}

	result, err := th.Generate(context.Background(), img, "image/jpeg", sizes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 thumbnails, got %d", len(result))
	}

	if _, ok := result["50x50"]; !ok {
		t.Error("expected 50x50 thumbnail")
	}
	if _, ok := result["100x100"]; !ok {
		t.Error("expected 100x100 thumbnail")
	}

	// Verify thumbnails are smaller than original
	for size, thumbData := range result {
		if int64(len(thumbData)) >= int64(len(data)) {
			t.Errorf("thumbnail %s size %d >= original %d", size, len(thumbData), len(data))
		}
	}
}

func TestThumbnailer_Generate_Crop(t *testing.T) {
	th := NewThumbnailer()

	// Create a 300x200 image (non-square)
	img := image.NewRGBA(image.Rect(0, 0, 300, 200))

	sizes := []domain.ThumbnailSize{
		{Width: 100, Height: 100},
	}

	result, err := th.Generate(context.Background(), img, "image/jpeg", sizes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result["100x100"]; !ok {
		t.Error("expected 100x100 thumbnail")
	}
}

func TestThumbnailer_Generate_CancelledContext(t *testing.T) {
	th := NewThumbnailer()
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := th.Generate(ctx, img, "image/jpeg", []domain.ThumbnailSize{{Width: 100, Height: 100}})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDetectImageFormat_JPEG(t *testing.T) {
	data := testJPEGHeader()
	got := DetectImageFormat(data)
	if got != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", got)
	}
}

func TestDetectImageFormat_PNG(t *testing.T) {
	data := testPNGHeader()
	got := DetectImageFormat(data)
	if got != "image/png" {
		t.Errorf("expected image/png, got %s", got)
	}
}

func TestDetectImageFormat_WebP(t *testing.T) {
	data := testWebPHeader()
	got := DetectImageFormat(data)
	if got != "image/webp" {
		t.Errorf("expected image/webp, got %s", got)
	}
}

func TestDetectImageFormat_Unknown(t *testing.T) {
	data := []byte("NOT_AN_IMAGE")
	got := DetectImageFormat(data)
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestDecodeImage_JPEG(t *testing.T) {
	buf := new(bytes.Buffer)
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	jpeg.Encode(buf, img, &jpeg.Options{Quality: 90})

	decoded, format, err := DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", format)
	}
	if decoded == nil {
		t.Fatal("expected decoded image")
	}
}

func TestDecodeImage_PNG(t *testing.T) {
	buf := new(bytes.Buffer)
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	png.Encode(buf, img)

	decoded, format, err := DecodeImage(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "image/png" {
		t.Errorf("expected image/png, got %s", format)
	}
	if decoded == nil {
		t.Fatal("expected decoded image")
	}
}

func testJPEGHeader() []byte {
	return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
}

func testPNGHeader() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
}

func testWebPHeader() []byte {
	return []byte("RIFF\x24\x00\x00\x00WEBPVP8 ")
}
