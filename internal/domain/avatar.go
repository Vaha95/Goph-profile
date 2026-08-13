package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Avatar struct {
	ID               uuid.UUID  `db:"id" json:"id"`
	UserID           string     `db:"user_id" json:"user_id"`
	FileName         string     `db:"file_name" json:"file_name"`
	MimeType         string     `db:"mime_type" json:"mime_type"`
	SizeBytes        int64      `db:"size_bytes" json:"size_bytes"`
	S3Key            string     `db:"s3_key" json:"-"`
	ThumbnailS3Keys  JSONMap    `db:"thumbnail_s3_keys" json:"thumbnail_s3_keys,omitempty"`
	UploadStatus     string     `db:"upload_status" json:"upload_status"`
	ProcessingStatus string     `db:"processing_status" json:"processing_status"`
	CreatedAt        time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at" json:"updated_at"`
	DeletedAt        *time.Time `db:"deleted_at" json:"deleted_at,omitempty"`
}

// JSONMap is a map that can be stored in and scanned from a JSONB/JSON column.
// lib/pq does not know how to scan JSONB into a plain map[string]string, so we
// provide explicit driver.Valuer and sql.Scanner implementations.
type JSONMap map[string]string

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		m = JSONMap{}
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}

	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("unsupported Scan, storing driver.Value type %T into type JSONMap", src)
	}

	if len(data) == 0 {
		*m = JSONMap{}
		return nil
	}

	var mm map[string]string
	if err := json.Unmarshal(data, &mm); err != nil {
		return fmt.Errorf("unmarshal JSONMap: %w", err)
	}
	*m = JSONMap(mm)
	return nil
}

const (
	StatusUploading = "uploading"
	StatusUploaded  = "uploaded"
	StatusFailed    = "failed"

	ProcessingPending    = "pending"
	ProcessingInProgress = "in_progress"
	ProcessingComplete   = "complete"
	ProcessingFailed     = "failed"
)
