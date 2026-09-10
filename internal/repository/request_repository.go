package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"approval-engine-service/internal/domain"
)

// RequestRepository persists running requests: the request itself, its
// materialised steps, and the approver snapshots (assignments) on each step.
type RequestRepository struct {
	db *sql.DB
}

func NewRequestRepository(db *sql.DB) *RequestRepository {
	return &RequestRepository{db: db}
}

func (r *RequestRepository) CreateRequest(ctx context.Context, req *domain.ApprovalRequest) error {
	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO approval_requests (id, app_id, definition_id, doc_type, resource_id, requester_id, payload, status, current_step_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.AppID, req.DefinitionID, req.DocType, req.ResourceID, req.RequesterID,
		string(payloadJSON), req.Status, req.CurrentStepOrder)
	if err != nil {
		return fmt.Errorf("insert request: %w", err)
	}
	return nil
}

// GetByID loads a request with its steps and each step's assignments.
func (r *RequestRepository) GetByID(ctx context.Context, id string) (*domain.ApprovalRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, app_id, definition_id, doc_type, resource_id, requester_id, payload, status, current_step_order, created_at, completed_at
		FROM approval_requests WHERE id = ?`, id)

	req, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	req.Steps, err = r.listSteps(ctx, id)
	if err != nil {
		return nil, err
	}
	return req, nil
}

// FindByResource looks up a request by the consuming app's own identifiers,
// which is how a create-call is made idempotent.
func (r *RequestRepository) FindByResource(ctx context.Context, appID, docType, resourceID string) (*domain.ApprovalRequest, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, app_id, definition_id, doc_type, resource_id, requester_id, payload, status, current_step_order, created_at, completed_at
		FROM approval_requests WHERE app_id = ? AND doc_type = ? AND resource_id = ?`, appID, docType, resourceID)

	req, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	req.Steps, err = r.listSteps(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return req, nil
}

// ListInbox returns pending assignments for one approver, newest first, each
// joined with its parent request so the UI has enough context in one call.
type InboxItem struct {
	Assignment domain.ApprovalAssignment `json:"assignment"`
	Request    domain.ApprovalRequest    `json:"request"`
	StepName   string                    `json:"step_name"`
	// TotalSteps is the step count of the workflow version this request runs
	// on — enough for a UI to show "step 2 of 4" without an extra request
	// per row (fetching each request's full detail here would be N+1).
	TotalSteps int `json:"total_steps"`
}

func (r *RequestRepository) ListInbox(ctx context.Context, userID string) ([]InboxItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.id, a.step_id, a.request_id, a.user_id, a.user_name, a.user_position, a.status, a.comment, a.acted_at, a.created_at,
		       s.name,
		       req.id, req.app_id, req.definition_id, req.doc_type, req.resource_id, req.requester_id, req.payload, req.status, req.current_step_order, req.created_at, req.completed_at,
		       (SELECT COUNT(*) FROM workflow_steps ws WHERE ws.definition_id = req.definition_id) AS total_steps
		FROM approval_assignments a
		JOIN approval_steps s ON s.id = a.step_id
		JOIN approval_requests req ON req.id = a.request_id
		WHERE a.user_id = ? AND a.status = 'pending'
		ORDER BY a.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list inbox: %w", err)
	}
	defer rows.Close()

	var out []InboxItem
	for rows.Next() {
		var item InboxItem
		var payloadJSON string
		var comment, actedAt, completedAt sql.NullString
		var assignmentCreatedAt string

		err := rows.Scan(
			&item.Assignment.ID, &item.Assignment.StepID, &item.Assignment.RequestID, &item.Assignment.UserID,
			&item.Assignment.UserName, &item.Assignment.UserPosition, &item.Assignment.Status, &comment, &actedAt, &assignmentCreatedAt,
			&item.StepName,
			&item.Request.ID, &item.Request.AppID, &item.Request.DefinitionID, &item.Request.DocType, &item.Request.ResourceID,
			&item.Request.RequesterID, &payloadJSON, &item.Request.Status, &item.Request.CurrentStepOrder,
			&item.Request.CreatedAt, &completedAt, &item.TotalSteps,
		)
		if err != nil {
			return nil, fmt.Errorf("scan inbox row: %w", err)
		}
		if comment.Valid {
			item.Assignment.Comment = &comment.String
		}
		if actedAt.Valid {
			item.Assignment.ActedAt = &actedAt.String
		}
		if completedAt.Valid {
			item.Request.CompletedAt = &completedAt.String
		}
		if err := json.Unmarshal([]byte(payloadJSON), &item.Request.Payload); err != nil {
			return nil, fmt.Errorf("unmarshal payload: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *RequestRepository) UpdateRequestStatus(ctx context.Context, id, status string, completedAt *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE approval_requests SET status = ?, completed_at = ? WHERE id = ?`, status, completedAt, id)
	if err != nil {
		return fmt.Errorf("update request status: %w", err)
	}
	return nil
}

func (r *RequestRepository) UpdateCurrentStep(ctx context.Context, id string, stepOrder int) error {
	_, err := r.db.ExecContext(ctx, `UPDATE approval_requests SET current_step_order = ? WHERE id = ?`, stepOrder, id)
	if err != nil {
		return fmt.Errorf("update current step: %w", err)
	}
	return nil
}

func (r *RequestRepository) CreateStep(ctx context.Context, step *domain.ApprovalStep) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO approval_steps (id, request_id, step_order, name, approval_mode, status, activated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		step.ID, step.RequestID, step.StepOrder, step.Name, step.ApprovalMode, step.Status, step.ActivatedAt, step.CompletedAt)
	if err != nil {
		return fmt.Errorf("insert step: %w", err)
	}
	return nil
}

func (r *RequestRepository) UpdateStepStatus(ctx context.Context, stepID, status string, completedAt *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE approval_steps SET status = ?, completed_at = ? WHERE id = ?`, status, completedAt, stepID)
	if err != nil {
		return fmt.Errorf("update step status: %w", err)
	}
	return nil
}

func (r *RequestRepository) listSteps(ctx context.Context, requestID string) ([]domain.ApprovalStep, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, request_id, step_order, name, approval_mode, status, activated_at, completed_at
		FROM approval_steps WHERE request_id = ? ORDER BY step_order`, requestID)
	if err != nil {
		return nil, fmt.Errorf("list steps: %w", err)
	}
	defer rows.Close()

	var out []domain.ApprovalStep
	for rows.Next() {
		var s domain.ApprovalStep
		var activatedAt, completedAt sql.NullString
		if err := rows.Scan(&s.ID, &s.RequestID, &s.StepOrder, &s.Name, &s.ApprovalMode, &s.Status, &activatedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		if activatedAt.Valid {
			s.ActivatedAt = &activatedAt.String
		}
		if completedAt.Valid {
			s.CompletedAt = &completedAt.String
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		assignments, err := r.ListAssignmentsByStep(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Assignments = assignments
	}
	return out, nil
}

func (r *RequestRepository) CreateAssignment(ctx context.Context, a *domain.ApprovalAssignment) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO approval_assignments (id, step_id, request_id, user_id, user_name, user_position, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.StepID, a.RequestID, a.UserID, nullableString(a.UserName), nullableString(a.UserPosition), a.Status)
	if err != nil {
		return fmt.Errorf("insert assignment: %w", err)
	}
	return nil
}

// GetAssignmentByID looks up one assignment directly by its own id — used by
// the QR/verify endpoints, which only have the assignment id (from the URL a
// printed document's barcode encodes).
func (r *RequestRepository) GetAssignmentByID(ctx context.Context, id string) (*domain.ApprovalAssignment, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, step_id, request_id, user_id, user_name, user_position, status, comment, acted_at
		FROM approval_assignments WHERE id = ?`, id)

	a, err := scanAssignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (r *RequestRepository) GetAssignment(ctx context.Context, stepID, userID string) (*domain.ApprovalAssignment, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, step_id, request_id, user_id, user_name, user_position, status, comment, acted_at
		FROM approval_assignments WHERE step_id = ? AND user_id = ?`, stepID, userID)

	a, err := scanAssignment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (r *RequestRepository) ListAssignmentsByStep(ctx context.Context, stepID string) ([]domain.ApprovalAssignment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, step_id, request_id, user_id, user_name, user_position, status, comment, acted_at
		FROM approval_assignments WHERE step_id = ?`, stepID)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	var out []domain.ApprovalAssignment
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *RequestRepository) UpdateAssignmentDecision(ctx context.Context, id, status string, comment *string, actedAt string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE approval_assignments SET status = ?, comment = ?, acted_at = ? WHERE id = ?`,
		status, comment, actedAt, id)
	if err != nil {
		return fmt.Errorf("update assignment decision: %w", err)
	}
	return nil
}

// SkipPendingAssignments closes out the other approvers on a step once it has
// already been decided (an "any" step approved by one person, or a rejection),
// so the inbox does not keep asking people to act on a step that is done.
func (r *RequestRepository) SkipPendingAssignments(ctx context.Context, stepID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE approval_assignments SET status = 'skipped' WHERE step_id = ? AND status = 'pending'`, stepID)
	if err != nil {
		return fmt.Errorf("skip pending assignments: %w", err)
	}
	return nil
}

func scanRequest(row rowScanner) (*domain.ApprovalRequest, error) {
	var req domain.ApprovalRequest
	var payloadJSON string
	var completedAt sql.NullString

	if err := row.Scan(&req.ID, &req.AppID, &req.DefinitionID, &req.DocType, &req.ResourceID, &req.RequesterID,
		&payloadJSON, &req.Status, &req.CurrentStepOrder, &req.CreatedAt, &completedAt); err != nil {
		return nil, err
	}
	if completedAt.Valid {
		req.CompletedAt = &completedAt.String
	}
	if err := json.Unmarshal([]byte(payloadJSON), &req.Payload); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return &req, nil
}

func scanAssignment(row rowScanner) (*domain.ApprovalAssignment, error) {
	var a domain.ApprovalAssignment
	var userName, userPosition, comment, actedAt sql.NullString

	if err := row.Scan(&a.ID, &a.StepID, &a.RequestID, &a.UserID, &userName, &userPosition, &a.Status, &comment, &actedAt); err != nil {
		return nil, err
	}
	a.UserName = userName.String
	a.UserPosition = userPosition.String
	if comment.Valid {
		a.Comment = &comment.String
	}
	if actedAt.Valid {
		a.ActedAt = &actedAt.String
	}
	return &a, nil
}
