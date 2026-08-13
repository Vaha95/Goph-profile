package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ServerHost string
	ServerPort int

	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string

	S3Endpoint     string
	S3Region       string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3UsePathStyle bool
	S3Scheme       string

	RMQURL      string
	RMQExchange string
	RMQQueue    string

	MaxUploadSize int64

	CORSAllowedOrigins string
	DBSSLMode          string
}

func Load() (*Config, error) {
	port, err := getEnvIntOrErr("SERVER_PORT", 8080)
	if err != nil {
		return nil, fmt.Errorf("SERVER_PORT: %w", err)
	}
	dbPort, err := getEnvIntOrErr("DB_PORT", 5432)
	if err != nil {
		return nil, fmt.Errorf("DB_PORT: %w", err)
	}
	usePathStyle, err := getEnvBoolOrErr("S3_USE_PATH_STYLE", true)
	if err != nil {
		return nil, fmt.Errorf("S3_USE_PATH_STYLE: %w", err)
	}
	maxUploadSize, err := getEnvInt64OrErr("MAX_UPLOAD_SIZE", 10*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("MAX_UPLOAD_SIZE: %w", err)
	}
	corsOrigins := getEnv("CORS_ALLOWED_ORIGINS", "")
	if corsOrigins == "" {
		corsOrigins = "http://localhost:3000"
	}
	if corsOrigins == "*" {
		return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS must be an explicit list, wildcard is not supported")
	}

	cfg := &Config{
		ServerHost:         getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:         port,
		DBHost:             getEnv("DB_HOST", "localhost"),
		DBPort:             dbPort,
		DBName:             getEnv("DB_NAME", "avatars"),
		DBUser:             getEnv("DB_USER", "postgres"),
		DBPassword:         getEnv("DB_PASSWORD", "postgres"),
		DBSSLMode:          getEnv("DB_SSL_MODE", "disable"),
		S3Endpoint:         getEnv("S3_ENDPOINT", "localhost:9000"),
		S3Region:           getEnv("S3_REGION", "us-east-1"),
		S3Bucket:           getEnv("S3_BUCKET", "avatars"),
		S3AccessKey:        getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey:        getEnv("S3_SECRET_KEY", ""),
		S3UsePathStyle:     usePathStyle,
		S3Scheme:           getEnv("S3_SCHEME", "http"),
		RMQURL:             getEnv("RMQ_URL", "amqp://guest:guest@localhost:5672"),
		RMQExchange:        getEnv("RMQ_EXCHANGE", "avatars.exchange"),
		RMQQueue:           getEnv("RMQ_QUEUE", "avatars.processing"),
		MaxUploadSize:      maxUploadSize,
		CORSAllowedOrigins: corsOrigins,
	}

	if cfg.S3AccessKey == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY is required")
	}
	if cfg.S3SecretKey == "" {
		return nil, fmt.Errorf("S3_SECRET_KEY is required")
	}

	return cfg, nil
}

func (c *Config) DBDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

func (c *Config) S3EndpointWithScheme() string {
	scheme := c.S3Scheme
	if scheme == "" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, c.S3Endpoint)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvIntOrErr(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q for %s: %w", v, key, err)
	}
	return n, nil
}

func getEnvInt64OrErr(key string, fallback int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q for %s: %w", v, key, err)
	}
	return n, nil
}

func getEnvBoolOrErr(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid boolean %q for %s: %w", v, key, err)
	}
	return b, nil
}
