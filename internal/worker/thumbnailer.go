package worker

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"

	"github.com/disintegration/imaging"
	"github.com/gophprofile/avatars-service/internal/domain"
)

type Thumbnailer interface {
	Generate(ctx context.Context, src image.Image, srcFormat string, sizes []domain.ThumbnailSize) (map[string][]byte, error)
}

type thumbnailer struct{}

func NewThumbnailer() Thumbnailer {
	return &thumbnailer{}
}

func (t *thumbnailer) Generate(ctx context.Context, src image.Image, srcFormat string, sizes []domain.ThumbnailSize) (map[string][]byte, error) {
	results := make(map[string][]byte)

	for _, sz := range sizes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		img := t.resizeImage(src, sz.Width, sz.Height)
		data, err := encodeImage(img)
		if err != nil {
			return nil, fmt.Errorf("generate thumbnail %dx%d: %w", sz.Width, sz.Height, err)
		}

		key := fmt.Sprintf("%dx%d", sz.Width, sz.Height)
		results[key] = data
	}

	return results, nil
}

func (t *thumbnailer) resizeImage(src image.Image, width, height int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if float64(srcW)/float64(srcH) != float64(width)/float64(height) {
		cropW := srcW
		cropH := int(float64(srcW) * float64(height) / float64(width))
		if cropH > srcH {
			cropH = srcH
			cropW = int(float64(srcH) * float64(width) / float64(height))
		}

		cropX := (srcW - cropW) / 2
		cropY := (srcH - cropH) / 2
		src = imaging.Crop(src, image.Rect(cropX, cropY, cropX+cropW, cropY+cropH))
	}

	return imaging.Resize(src, width, height, imaging.Lanczos)
}

func encodeImage(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}
	return buf.Bytes(), nil
}

// CheckImageDimensions parses only the image header (no full decode) and
// rejects images whose dimensions exceed maxDimension, so oversized files are
// not fully decoded into memory.
func CheckImageDimensions(data []byte, maxDimension int) error {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode image config: %w", err)
	}
	if cfg.Width > maxDimension || cfg.Height > maxDimension {
		return fmt.Errorf("image too large %dx%d (max %d)", cfg.Width, cfg.Height, maxDimension)
	}
	return nil
}

func DetectImageFormat(data []byte) string {
	format := ""
	magic := data
	if len(data) > 16 {
		magic = data[:16]
	}

	switch {
	case len(magic) >= 3 && magic[0] == 0xFF && magic[1] == 0xD8 && magic[2] == 0xFF:
		format = "image/jpeg"
	case len(magic) >= 8 && magic[0] == 0x89 && magic[1] == 0x50 && magic[2] == 0x4E && magic[3] == 0x47:
		format = "image/png"
	case len(magic) >= 12 && string(magic[8:12]) == "WEBP":
		format = "image/webp"
	}
	return format
}

func DecodeImage(data []byte) (image.Image, string, error) {
	format := DetectImageFormat(data)
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	return img, format, nil
}
