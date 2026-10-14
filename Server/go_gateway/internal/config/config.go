package config

import (
	"fmt"
	"os"
	"strconv"
)

const (
	ModeDataPlane    = "DATA_PLANE"
	ModeControlPlane = "CONTROL_PLANE"
	ModeUnified      = "UNIFIED"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	Port                string
	ServerMode          string
	DatabaseURL         string
	DBMaxOpenConns      int
	DBMaxIdleConns      int
	FirebaseProjectID   string
	ServiceAccountEmail string
	GCSBucket           string
}

// Load reads environment variables and returns a validated Config.
// It returns an error if any required variable is missing or invalid.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                getEnvOrDefault("PORT", "8080"),
		ServerMode:          os.Getenv("SERVER_MODE"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		FirebaseProjectID:   os.Getenv("FIREBASE_PROJECT_ID"),
		ServiceAccountEmail: os.Getenv("SERVICE_ACCOUNT_EMAIL"),
		GCSBucket:           os.Getenv("GCS_BUCKET"),
	}

	var err error
	cfg.DBMaxOpenConns, err = getEnvInt("DB_MAX_OPEN_CONNS", 5)
	if err != nil {
		return nil, fmt.Errorf("DB_MAX_OPEN_CONNS: %w", err)
	}
	cfg.DBMaxIdleConns, err = getEnvInt("DB_MAX_IDLE_CONNS", 2)
	if err != nil {
		return nil, fmt.Errorf("DB_MAX_IDLE_CONNS: %w", err)
	}

	if cfg.ServerMode != ModeDataPlane && cfg.ServerMode != ModeControlPlane && cfg.ServerMode != ModeUnified {
		return nil, fmt.Errorf("SERVER_MODE must be %q, %q, or %q, got %q", ModeDataPlane, ModeControlPlane, ModeUnified, cfg.ServerMode)
	}
	if cfg.FirebaseProjectID == "" {
		return nil, fmt.Errorf("FIREBASE_PROJECT_ID is required")
	}
	if cfg.ServiceAccountEmail == "" {
		return nil, fmt.Errorf("SERVICE_ACCOUNT_EMAIL is required")
	}
	if cfg.GCSBucket == "" {
		return nil, fmt.Errorf("GCS_BUCKET is required")
	}
	if (cfg.ServerMode == ModeControlPlane || cfg.ServerMode == ModeUnified) && cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required when SERVER_MODE=%s", cfg.ServerMode)
	}

	return cfg, nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("must be an integer, got %q", v)
	}
	return n, nil
}
