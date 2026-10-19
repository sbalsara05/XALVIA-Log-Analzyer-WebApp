package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"

	"ricovr/gateway/internal/config"
)

// Connect opens a Postgres connection pool using settings from cfg.
// SetMaxOpenConns and SetMaxIdleConns are applied before Ping so Cloud Run
// instances never exceed the configured connection budget on the external DB.
func Connect(cfg *config.Config) (*sql.DB, error) {
	pool, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	pool.SetMaxOpenConns(cfg.DBMaxOpenConns)
	pool.SetMaxIdleConns(cfg.DBMaxIdleConns)

	if err := pool.Ping(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}
