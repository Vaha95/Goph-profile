package services

import (
	"testing"
)

func TestRoutingKeys(t *testing.T) {
	if RoutingKeyUpload != "avatar.uploaded" {
		t.Errorf("expected avatar.uploaded, got %s", RoutingKeyUpload)
	}
}
