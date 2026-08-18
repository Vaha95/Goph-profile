package domain

import (
	"testing"
)

func TestThumbnailSize_String(t *testing.T) {
	ts := ThumbnailSize{Width: 300, Height: 200}
	if ts.String() != "300x200" {
		t.Errorf("expected 300x200, got %s", ts.String())
	}
}

func TestThumbnailSize_StringWithSep(t *testing.T) {
	ts := ThumbnailSize{Width: 100, Height: 100}
	got := ts.StringWithSep("_")
	if got != "100_100" {
		t.Errorf("expected 100_100, got %s", got)
	}
}

func TestDefaultThumbnailSizes(t *testing.T) {
	if len(DefaultThumbnailSizes) != 2 {
		t.Errorf("expected 2 default sizes, got %d", len(DefaultThumbnailSizes))
	}
	if DefaultThumbnailSizes[0].Width != 100 {
		t.Error("expected first size width 100")
	}
	if DefaultThumbnailSizes[1].Width != 300 {
		t.Error("expected second size width 300")
	}
}

func TestStatusConstants(t *testing.T) {
	if StatusUploading != "uploading" {
		t.Error("expected uploading")
	}
	if StatusUploaded != "uploaded" {
		t.Error("expected uploaded")
	}
	if StatusFailed != "failed" {
		t.Error("expected failed")
	}
	if ProcessingPending != "pending" {
		t.Error("expected pending")
	}
	if ProcessingInProgress != "in_progress" {
		t.Error("expected in_progress")
	}
	if ProcessingComplete != "complete" {
		t.Error("expected complete")
	}
	if ProcessingFailed != "failed" {
		t.Error("expected failed")
	}
}
