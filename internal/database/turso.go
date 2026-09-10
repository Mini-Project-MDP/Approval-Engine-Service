package database

import (
	"database/sql"
	"fmt"

	_ "github.com/tursodatabase/libsql-client-go/libsql"

	"approval-engine-service/internal/config"
)

// NewTurso opens a database/sql connection to a Turso (libsql) database.
// Callers should check config.HasDatabase() first if a DB is optional.
func NewTurso(cfg *config.Config) (*sql.DB, error) {
	dsn := cfg.DatabaseURL
	if cfg.DatabaseAuthToken != "" {
		dsn = fmt.Sprintf("%s?authToken=%s", cfg.DatabaseURL, cfg.DatabaseAuthToken)
	}

	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open turso connection: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping turso: %w", err)
	}

	return db, nil
}
