package domain

import (
	"fmt"
)

type AvatarUploadEvent struct {
	AvatarID string `json:"avatar_id"`
	UserID   string `json:"user_id"`
	S3Key    string `json:"s3_key"`
}

type ThumbnailSize struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

var DefaultThumbnailSizes = []ThumbnailSize{
	{Width: 100, Height: 100},
	{Width: 300, Height: 300},
}

func (ts ThumbnailSize) String() string {
	return ts.StringWithSep("x")
}

func (ts ThumbnailSize) StringWithSep(sep string) string {
	return fmt.Sprintf("%d%s%d", ts.Width, sep, ts.Height)
}
