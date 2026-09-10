package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"approval-engine-service/internal/domain"
)

// EventRepository appends to the audit trail. Rows are never updated or
// deleted — approval_requests/approval_steps answer "what is true now",
// this table answers "what happened", permanently.
type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) Append(ctx context.Context, e *domain.ApprovalEvent) error {
	detailJSON, err := json.Marshal(e.Detail)
	if err != nil {
		return fmt.Errorf("marshal event detail: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO approval_events (id, request_id, step_id, actor_id, event_type, detail)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.RequestID, e.StepID, e.ActorID, e.EventType, string(detailJSON))
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (r *EventRepository) ListByRequest(ctx context.Context, requestID string) ([]domain.ApprovalEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, request_id, step_id, actor_id, event_type, detail, created_at
		FROM approval_events WHERE request_id = ? ORDER BY created_at`, requestID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	// Initialized, not nil — see application_repository.go's List for why.
	out := []domain.ApprovalEvent{}
	for rows.Next() {
		var ev domain.ApprovalEvent
		var stepID, actorID sql.NullString
		var detailJSON string

		if err := rows.Scan(&ev.ID, &ev.RequestID, &stepID, &actorID, &ev.EventType, &detailJSON, &ev.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if stepID.Valid {
			ev.StepID = &stepID.String
		}
		if actorID.Valid {
			ev.ActorID = &actorID.String
		}
		if err := json.Unmarshal([]byte(detailJSON), &ev.Detail); err != nil {
			return nil, fmt.Errorf("unmarshal event detail: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
