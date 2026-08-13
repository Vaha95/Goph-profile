package repository

import (
	"database/sql"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestRepository_Interface(t *testing.T) {
	var _ AvatarRepository = (*avatarRepo)(nil)
}

func TestNewAvatarRepository(t *testing.T) {
	db := sqlx.NewDb(&sql.DB{}, "postgres")
	repo := NewAvatarRepository(db)
	if repo == nil {
		t.Fatal("expected non-nil repository")
	}
}
