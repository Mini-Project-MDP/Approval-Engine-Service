package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"approval-engine-service/internal/domain"
)

// ApplicationRepository backs API-key authentication for consuming apps
// (e.g. an asset management Spring Boot service).
type ApplicationRepository struct {
	db *sql.DB
}

func NewApplicationRepository(db *sql.DB) *ApplicationRepository {
	return &ApplicationRepository{db: db}
}

// GetByAPIKey returns the application id for a key, or ("", false, nil) if
// the key does not match any application.
func (r *ApplicationRepository) GetByAPIKey(ctx context.Context, apiKey string) (id string, isActive bool, err error) {
	var active int64
	err = r.db.QueryRowContext(ctx, `SELECT id, is_active FROM applications WHERE api_key = ?`, apiKey).Scan(&id, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lookup application by api key: %w", err)
	}
	return id, active != 0, nil
}

// Create registers a new consuming application. The caller is responsible
// for generating a unique id and a random api key before calling this.
func (r *ApplicationRepository) Create(ctx context.Context, app *domain.Application) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO applications (id, code, name, api_key, is_active) VALUES (?, ?, ?, ?, ?)`,
		app.ID, app.Code, app.Name, app.APIKey, boolToInt(app.IsActive))
	if err != nil {
		return fmt.Errorf("insert application: %w", err)
	}
	return nil
}

func (r *ApplicationRepository) List(ctx context.Context) ([]domain.Application, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, api_key, is_active, created_at FROM applications ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()

	var out []domain.Application
	for rows.Next() {
		var a domain.Application
		var isActive int64
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.APIKey, &isActive, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan application: %w", err)
		}
		a.IsActive = isActive != 0
		out = append(out, a)
	}
	return out, rows.Err()
}
