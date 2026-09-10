package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear any env vars that might be set
	for _, v := range []string{
		"SERVER_HOST", "SERVER_PORT", "DB_HOST", "DB_PORT", "DB_NAME",
		"DB_USER", "DB_PASSWORD", "S3_ENDPOINT", "S3_REGION", "S3_BUCKET",
		"S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_USE_PATH_STYLE", "RMQ_URL",
		"RMQ_EXCHANGE", "RMQ_QUEUE", "MAX_UPLOAD_SIZE", "CORS_ALLOWED_ORIGINS",
	} {
		os.Unsetenv(v)
	}

	// S3 keys are required, so set them
	os.Setenv("S3_ACCESS_KEY", "testkey")
	os.Setenv("S3_SECRET_KEY", "testsecret")
	defer func() {
		os.Unsetenv("S3_ACCESS_KEY")
		os.Unsetenv("S3_SECRET_KEY")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ServerHost != "0.0.0.0" {
		t.Errorf("expected 0.0.0.0, got %s", cfg.ServerHost)
	}
	if cfg.ServerPort != 8080 {
		t.Errorf("expected 8080, got %d", cfg.ServerPort)
	}
	if cfg.DBHost != "localhost" {
		t.Errorf("expected localhost, got %s", cfg.DBHost)
	}
	if cfg.DBPort != 5432 {
		t.Errorf("expected 5432, got %d", cfg.DBPort)
	}
	if cfg.MaxUploadSize != 10*1024*1024 {
		t.Errorf("expected 10MB, got %d", cfg.MaxUploadSize)
	}
}

func TestLoad_MissingS3Key(t *testing.T) {
	for _, v := range []string{
		"S3_ACCESS_KEY", "S3_SECRET_KEY",
	} {
		os.Unsetenv(v)
	}
	os.Setenv("S3_SECRET_KEY", "secret")
	defer func() {
		os.Unsetenv("S3_ACCESS_KEY")
		os.Unsetenv("S3_SECRET_KEY")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing S3_ACCESS_KEY")
	}
}

func TestLoad_MissingS3Secret(t *testing.T) {
	for _, v := range []string{
		"S3_ACCESS_KEY", "S3_SECRET_KEY",
	} {
		os.Unsetenv(v)
	}
	os.Setenv("S3_ACCESS_KEY", "key")
	defer func() {
		os.Unsetenv("S3_ACCESS_KEY")
		os.Unsetenv("S3_SECRET_KEY")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing S3_SECRET_KEY")
	}
}

func TestLoad_CustomEnv(t *testing.T) {
	for _, v := range []string{
		"SERVER_HOST", "SERVER_PORT", "DB_HOST", "DB_PORT", "DB_NAME",
		"DB_USER", "DB_PASSWORD", "S3_ENDPOINT", "S3_REGION", "S3_BUCKET",
		"S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_USE_PATH_STYLE", "RMQ_URL",
		"RMQ_EXCHANGE", "RMQ_QUEUE", "MAX_UPLOAD_SIZE", "CORS_ALLOWED_ORIGINS",
	} {
		os.Unsetenv(v)
	}

	os.Setenv("SERVER_HOST", "127.0.0.1")
	os.Setenv("SERVER_PORT", "9090")
	os.Setenv("DB_HOST", "db.internal")
	os.Setenv("DB_PORT", "5433")
	os.Setenv("DB_NAME", "testdb")
	os.Setenv("DB_USER", "testuser")
	os.Setenv("DB_PASSWORD", "testpass")
	os.Setenv("S3_ENDPOINT", "s3.local:9000")
	os.Setenv("S3_REGION", "eu-west-1")
	os.Setenv("S3_BUCKET", "testbucket")
	os.Setenv("S3_ACCESS_KEY", "key")
	os.Setenv("S3_SECRET_KEY", "secret")
	os.Setenv("S3_USE_PATH_STYLE", "false")
	os.Setenv("RMQ_URL", "amqp://admin:pass@rmq:5672")
	os.Setenv("MAX_UPLOAD_SIZE", "5242880")
	os.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	defer func() {
		for _, v := range []string{
			"SERVER_HOST", "SERVER_PORT", "DB_HOST", "DB_PORT", "DB_NAME",
			"DB_USER", "DB_PASSWORD", "S3_ENDPOINT", "S3_REGION", "S3_BUCKET",
			"S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_USE_PATH_STYLE", "RMQ_URL",
			"RMQ_EXCHANGE", "RMQ_QUEUE", "MAX_UPLOAD_SIZE", "CORS_ALLOWED_ORIGINS",
		} {
			os.Unsetenv(v)
		}
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ServerHost != "127.0.0.1" {
		t.Errorf("expected 127.0.0.1, got %s", cfg.ServerHost)
	}
	if cfg.ServerPort != 9090 {
		t.Errorf("expected 9090, got %d", cfg.ServerPort)
	}
	if cfg.DBHost != "db.internal" {
		t.Errorf("expected db.internal, got %s", cfg.DBHost)
	}
	if cfg.S3UsePathStyle != false {
		t.Errorf("expected false, got %v", cfg.S3UsePathStyle)
	}
	if cfg.MaxUploadSize != 5242880 {
		t.Errorf("expected 5242880, got %d", cfg.MaxUploadSize)
	}
}

func TestConfig_DBDSN(t *testing.T) {
	cfg := &Config{
		DBUser:     "u",
		DBPassword: "p",
		DBHost:     "h",
		DBPort:     5433,
		DBName:     "d",
		DBSSLMode:  "disable",
	}

	dsn := cfg.DBDSN()
	expected := "postgres://u:p@h:5433/d?sslmode=disable"
	if dsn != expected {
		t.Errorf("expected %s, got %s", expected, dsn)
	}
}
