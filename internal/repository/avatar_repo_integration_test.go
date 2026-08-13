//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gophprofile/avatars-service/internal/domain"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// These tests spin up a real Postgres container and verify that the SQL layer
// (including JSONB scan/valuing) works against a live database. They are
// excluded from the default `go test ./...` run via the `integration` build
// tag; run them with:
//
//	RUN_INTEGRATION=1 go test -tags integration ./internal/repository/
func TestAvatarRepository_PostgresRoundTrip(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to run integration tests")
	}

	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image: "postgres:16-alpine",
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "avatars",
		},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor: wait.ForSQL("5432/tcp", "postgres", func(host string, port nat.Port) string {
			return fmt.Sprintf("postgres://postgres:postgres@%s:%s/avatars?sslmode=disable", host, port.Port())
		}).WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	defer container.Terminate(ctx)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("container port: %v", err)
	}

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/avatars?sslmode=disable", host, port.Port())
	db := sqlx.MustConnect("postgres", dsn)
	defer db.Close()

	migSQL, err := os.ReadFile("../../migrations/001_create_avatars.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(migSQL)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	repo := NewAvatarRepository(db)

	t.Run("CreateAndGetByID_JSONBRoundTrip", func(t *testing.T) {
		avatar := &domain.Avatar{
			UserID:           "user_42",
			FileName:         "photo.png",
			MimeType:         "image/png",
			SizeBytes:        2048,
			S3Key:            "user_42/<uuid>/original.png",
			ThumbnailS3Keys:  domain.JSONMap{"100x100": "user_42/<uuid>/100x100.jpg"},
			UploadStatus:     domain.StatusUploaded,
			ProcessingStatus: domain.ProcessingPending,
		}
		if err := repo.Create(ctx, avatar); err != nil {
			t.Fatalf("create: %v", err)
		}
		if avatar.ID == uuid.Nil {
			t.Fatal("expected avatar ID to be set after create")
		}

		got, err := repo.GetByID(ctx, avatar.ID)
		if err != nil {
			t.Fatalf("get by id: %v", err)
		}
		if got.UserID != avatar.UserID {
			t.Errorf("userID mismatch: %s != %s", got.UserID, avatar.UserID)
		}
		if len(got.ThumbnailS3Keys) != 1 || got.ThumbnailS3Keys["100x100"] != avatar.ThumbnailS3Keys["100x100"] {
			t.Errorf("thumbnail keys not round-tripped: %+v", got.ThumbnailS3Keys)
		}
	})

	t.Run("GetByID_NotFound", func(t *testing.T) {
		_, err := repo.GetByID(ctx, uuid.New())
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("UpdateProcessingStatus", func(t *testing.T) {
		avatar := newAvatar("user_43")
		if err := repo.Create(ctx, avatar); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := repo.UpdateProcessingStatus(ctx, avatar.ID, domain.ProcessingComplete); err != nil {
			t.Fatalf("update processing status: %v", err)
		}
		got, err := repo.GetByID(ctx, avatar.ID)
		if err != nil {
			t.Fatalf("get by id: %v", err)
		}
		if got.ProcessingStatus != domain.ProcessingComplete {
			t.Errorf("expected complete, got %s", got.ProcessingStatus)
		}
	})

	t.Run("UpdateThumbnailKeys", func(t *testing.T) {
		avatar := newAvatar("user_44")
		if err := repo.Create(ctx, avatar); err != nil {
			t.Fatalf("create: %v", err)
		}
		keys := map[string]string{
			"100x100": "user_44/abc/100x100.jpg",
			"300x300": "user_44/abc/300x300.jpg",
		}
		if err := repo.UpdateThumbnailKeys(ctx, avatar.ID, keys); err != nil {
			t.Fatalf("update thumbnail keys: %v", err)
		}
		got, err := repo.GetByID(ctx, avatar.ID)
		if err != nil {
			t.Fatalf("get by id: %v", err)
		}
		if got.ProcessingStatus != domain.ProcessingComplete {
			t.Errorf("expected complete, got %s", got.ProcessingStatus)
		}
		if len(got.ThumbnailS3Keys) != 2 || got.ThumbnailS3Keys["300x300"] != keys["300x300"] {
			t.Errorf("thumbnail keys mismatch: %+v", got.ThumbnailS3Keys)
		}
	})

	t.Run("GetLatestAndListByUserID", func(t *testing.T) {
		userID := "user_45"
		for i := 0; i < 3; i++ {
			if err := repo.Create(ctx, newAvatar(userID)); err != nil {
				t.Fatalf("create: %v", err)
			}
			time.Sleep(5 * time.Millisecond) // keep created_at ordering deterministic
		}

		latest, err := repo.GetLatestByUserID(ctx, userID)
		if err != nil {
			t.Fatalf("get latest: %v", err)
		}
		if latest.UserID != userID {
			t.Errorf("expected user %s, got %s", userID, latest.UserID)
		}

		avatars, err := repo.ListByUserID(ctx, userID, 10, 0)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(avatars) != 3 {
			t.Errorf("expected 3 avatars, got %d", len(avatars))
		}
	})

	t.Run("SoftDeleteAndGetDeletedInfo", func(t *testing.T) {
		avatar := newAvatar("user_46")
		if err := repo.Create(ctx, avatar); err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := repo.SoftDelete(ctx, avatar.ID); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
		if _, err := repo.GetByID(ctx, avatar.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound after delete, got %v", err)
		}
		deleted, err := repo.GetDeletedInfo(ctx, avatar.ID)
		if err != nil {
			t.Fatalf("get deleted info: %v", err)
		}
		if deleted.DeletedAt == nil {
			t.Error("expected deleted_at to be set")
		}
	})

	t.Run("SoftDeleteOwned", func(t *testing.T) {
		avatar := newAvatar("user_47")
		if err := repo.Create(ctx, avatar); err != nil {
			t.Fatalf("create: %v", err)
		}

		// Wrong owner: nothing deleted.
		if err := repo.SoftDeleteOwned(ctx, avatar.ID, "someone_else"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for wrong owner, got %v", err)
		}

		if err := repo.SoftDeleteOwned(ctx, avatar.ID, "user_47"); err != nil {
			t.Fatalf("soft delete owned: %v", err)
		}
		if _, err := repo.GetByID(ctx, avatar.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound after owned delete, got %v", err)
		}
	})
}

func newAvatar(userID string) *domain.Avatar {
	return &domain.Avatar{
		UserID:           userID,
		FileName:         "avatar.jpg",
		MimeType:         "image/jpeg",
		SizeBytes:        1024,
		S3Key:            userID + "/x/avatar.jpg",
		ThumbnailS3Keys:  domain.JSONMap{},
		UploadStatus:     domain.StatusUploaded,
		ProcessingStatus: domain.ProcessingPending,
	}
}
