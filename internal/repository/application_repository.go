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
		INSERT INTO applications (id, code, name, api_key, callback_url, is_active) VALUES (?, ?, ?, ?, ?, ?)`,
		app.ID, app.Code, app.Name, app.APIKey, nullableString(app.CallbackURL), boolToInt(app.IsActive))
	if err != nil {
		return fmt.Errorf("insert application: %w", err)
	}
	return nil
}

func (r *ApplicationRepository) List(ctx context.Context, page, limit int) ([]domain.Application, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count applications: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, api_key, callback_url, is_active, created_at FROM applications ORDER BY name LIMIT ? OFFSET ?`,
		limit, (page-1)*limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list applications: %w", err)
	}
	defer rows.Close()

	// Initialized rather than a nil `var out []domain.Application`: a nil
	// slice marshals to JSON `null`, and response.Page.Items has no
	// `omitempty` — an empty page would send {"items": null} instead of
	// {"items": []}, which broke every list page's "still loading vs.
	// genuinely empty" check on the frontend (they use items === null as
	// the loading sentinel).
	out := []domain.Application{}
	for rows.Next() {
		var a domain.Application
		var isActive int64
		var callbackURL sql.NullString
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.APIKey, &callbackURL, &isActive, &a.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan application: %w", err)
		}
		a.CallbackURL = callbackURL.String
		a.IsActive = isActive != 0
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// GetByID returns one application with its callback_url and api_key
// (unlike List, which strips api_key for the management UI) — used by the
// webhook notifier to know where and how to sign a delivery for a given
// app id. Returns (nil, nil) if no application has that id.
func (r *ApplicationRepository) GetByID(ctx context.Context, id string) (*domain.Application, error) {
	var a domain.Application
	var isActive int64
	var callbackURL sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, api_key, callback_url, is_active, created_at FROM applications WHERE id = ?`, id).
		Scan(&a.ID, &a.Code, &a.Name, &a.APIKey, &callbackURL, &isActive, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get application %s: %w", id, err)
	}
	a.CallbackURL = callbackURL.String
	a.IsActive = isActive != 0
	return &a, nil
}
