package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"approval-engine-service/internal/domain"
)

// ParticipantRepository implements service.ParticipantStore against Turso,
// plus the write side used by the FICOM import endpoint.
type ParticipantRepository struct {
	db *sql.DB
}

func NewParticipantRepository(db *sql.DB) *ParticipantRepository {
	return &ParticipantRepository{db: db}
}

func (r *ParticipantRepository) GetByID(ctx context.Context, userID string) (*domain.Participant, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT user_id, name, email, position, department, superior_id, is_active, updated_at
		FROM participants WHERE user_id = ?`, userID)
	p, err := scanParticipant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *ParticipantRepository) FindByPosition(ctx context.Context, position, department string) ([]domain.Participant, error) {
	query := `SELECT user_id, name, email, position, department, superior_id, is_active, updated_at
		FROM participants WHERE position = ?`
	args := []any{position}
	if department != "" {
		query += " AND department = ?"
		args = append(args, department)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find by position: %w", err)
	}
	defer rows.Close()

	// Initialized, not nil — see application_repository.go's List for why.
	out := []domain.Participant{}
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// Upsert inserts or replaces a participant. This is the write path for the
// FICOM import: one row per employee, superior_id pointing at their NIK.
func (r *ParticipantRepository) Upsert(ctx context.Context, p domain.Participant) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO participants (user_id, name, email, position, department, superior_id, is_active, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(user_id) DO UPDATE SET
			name = excluded.name,
			email = excluded.email,
			position = excluded.position,
			department = excluded.department,
			superior_id = excluded.superior_id,
			is_active = excluded.is_active,
			updated_at = datetime('now')`,
		p.UserID, p.Name, nullableString(p.Email), nullableString(p.Position),
		nullableString(p.Department), p.SuperiorID, boolToInt(p.IsActive),
	)
	if err != nil {
		return fmt.Errorf("upsert participant %s: %w", p.UserID, err)
	}
	return nil
}

// UpsertBatch imports many participants (e.g. a full FICOM export) in one
// transaction, so a partial failure never leaves the table half-updated. A
// FICOM export has no guaranteed order, so a report can appear before their
// superior in the same batch; defer_foreign_keys checks superior_id against
// the finished batch at commit time instead of row by row.
func (r *ParticipantRepository) UpsertBatch(ctx context.Context, participants []domain.Participant) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable defer_foreign_keys: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO participants (user_id, name, email, position, department, superior_id, is_active, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(user_id) DO UPDATE SET
			name = excluded.name,
			email = excluded.email,
			position = excluded.position,
			department = excluded.department,
			superior_id = excluded.superior_id,
			is_active = excluded.is_active,
			updated_at = datetime('now')`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, p := range participants {
		if _, err := stmt.ExecContext(ctx,
			p.UserID, p.Name, nullableString(p.Email), nullableString(p.Position),
			nullableString(p.Department), p.SuperiorID, boolToInt(p.IsActive),
		); err != nil {
			return fmt.Errorf("upsert participant %s: %w", p.UserID, err)
		}
	}

	return tx.Commit()
}

func (r *ParticipantRepository) List(ctx context.Context, page, limit int) ([]domain.Participant, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM participants`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count participants: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT user_id, name, email, position, department, superior_id, is_active, updated_at
		FROM participants ORDER BY name LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	// Initialized, not nil — see application_repository.go's List for why.
	out := []domain.Participant{}
	for rows.Next() {
		p, err := scanParticipant(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *p)
	}
	return out, total, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanParticipant(row rowScanner) (*domain.Participant, error) {
	var p domain.Participant
	var email, position, department, superiorID, updatedAt sql.NullString
	var isActive int64

	if err := row.Scan(&p.UserID, &p.Name, &email, &position, &department, &superiorID, &isActive, &updatedAt); err != nil {
		return nil, err
	}

	p.Email = email.String
	p.Position = position.String
	p.Department = department.String
	p.IsActive = isActive != 0
	p.UpdatedAt = updatedAt.String
	if superiorID.Valid && superiorID.String != "" {
		v := superiorID.String
		p.SuperiorID = &v
	}
	return &p, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
