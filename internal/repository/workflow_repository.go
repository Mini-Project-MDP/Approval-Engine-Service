package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"approval-engine-service/internal/domain"
)

// WorkflowRepository reads/writes workflow blueprints. Definitions are
// versioned: GetActiveDefinition picks the current version for a new
// request, GetDefinitionByID re-loads the exact version an in-flight request
// started with, so a policy change never rewrites history under it.
type WorkflowRepository struct {
	db *sql.DB
}

func NewWorkflowRepository(db *sql.DB) *WorkflowRepository {
	return &WorkflowRepository{db: db}
}

func (r *WorkflowRepository) GetActiveDefinition(ctx context.Context, appID, docType string) (*domain.WorkflowDefinition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, app_id, doc_type, name, version, is_active, created_at
		FROM workflow_definitions
		WHERE app_id = ? AND doc_type = ? AND is_active = 1
		ORDER BY version DESC LIMIT 1`, appID, docType)

	def, err := scanDefinition(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	def.Steps, err = r.loadSteps(ctx, def.ID)
	if err != nil {
		return nil, err
	}
	return def, nil
}

func (r *WorkflowRepository) GetDefinitionByID(ctx context.Context, id string) (*domain.WorkflowDefinition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, app_id, doc_type, name, version, is_active, created_at
		FROM workflow_definitions WHERE id = ?`, id)

	def, err := scanDefinition(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	def.Steps, err = r.loadSteps(ctx, def.ID)
	if err != nil {
		return nil, err
	}
	return def, nil
}

func (r *WorkflowRepository) ListDefinitions(ctx context.Context, appID string) ([]domain.WorkflowDefinition, error) {
	query := `SELECT id, app_id, doc_type, name, version, is_active, created_at FROM workflow_definitions`
	var args []any
	if appID != "" {
		query += " WHERE app_id = ?"
		args = append(args, appID)
	}
	query += " ORDER BY doc_type, version DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list definitions: %w", err)
	}
	defer rows.Close()

	var out []domain.WorkflowDefinition
	for rows.Next() {
		def, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *def)
	}
	return out, rows.Err()
}

// CreateDefinition inserts a new blueprint together with its steps in one
// transaction. Used both by seeding and by the workflow CRUD API.
// CreateDefinition inserts a definition exactly as given (caller sets
// version/is_active). Used for one-shot seeding; the CRUD API uses Publish
// instead so version numbers and single-active-version are handled for it.
func (r *WorkflowRepository) CreateDefinition(ctx context.Context, def *domain.WorkflowDefinition) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if err := insertDefinitionTx(ctx, tx, def); err != nil {
		return err
	}
	return tx.Commit()
}

// Publish creates a new version of the app_id+doc_type workflow and makes it
// the only active one, atomically. In-flight requests are unaffected: they
// keep referencing the exact definition_id they started with (see
// GetDefinitionByID), regardless of which version becomes active here — that
// is what lets system-support edit a workflow without a code change and
// without disturbing approvals already in progress.
func (r *WorkflowRepository) Publish(ctx context.Context, def *domain.WorkflowDefinition) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var maxVersion sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(version) FROM workflow_definitions WHERE app_id = ? AND doc_type = ?`,
		def.AppID, def.DocType,
	).Scan(&maxVersion); err != nil {
		return fmt.Errorf("compute next version: %w", err)
	}
	def.Version = 1
	if maxVersion.Valid {
		def.Version = int(maxVersion.Int64) + 1
	}
	def.IsActive = true

	if _, err := tx.ExecContext(ctx,
		`UPDATE workflow_definitions SET is_active = 0 WHERE app_id = ? AND doc_type = ?`,
		def.AppID, def.DocType,
	); err != nil {
		return fmt.Errorf("deactivate previous versions: %w", err)
	}

	if err := insertDefinitionTx(ctx, tx, def); err != nil {
		return err
	}
	return tx.Commit()
}

// Deactivate turns off one specific version (e.g. an admin disabling a
// workflow without immediately replacing it). Never a hard delete: past
// requests still reference this definition_id and must stay readable.
func (r *WorkflowRepository) Deactivate(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE workflow_definitions SET is_active = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("deactivate definition: %w", err)
	}
	return nil
}

func insertDefinitionTx(ctx context.Context, tx *sql.Tx, def *domain.WorkflowDefinition) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_definitions (id, app_id, doc_type, name, version, is_active)
		VALUES (?, ?, ?, ?, ?, ?)`,
		def.ID, def.AppID, def.DocType, def.Name, def.Version, boolToInt(def.IsActive))
	if err != nil {
		return fmt.Errorf("insert definition: %w", err)
	}

	for _, step := range def.Steps {
		ruleJSON, err := json.Marshal(step.ResolverRule)
		if err != nil {
			return fmt.Errorf("marshal resolver_rule: %w", err)
		}
		var conditionJSON any
		if step.Condition != nil {
			b, err := json.Marshal(step.Condition)
			if err != nil {
				return fmt.Errorf("marshal condition: %w", err)
			}
			conditionJSON = string(b)
		}

		if _, err = tx.ExecContext(ctx, `
			INSERT INTO workflow_steps (id, definition_id, step_order, name, resolver_rule, condition, approval_mode, on_empty)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			step.ID, def.ID, step.StepOrder, step.Name, string(ruleJSON), conditionJSON, step.ApprovalMode, step.OnEmpty,
		); err != nil {
			return fmt.Errorf("insert step %d: %w", step.StepOrder, err)
		}
	}

	return nil
}

func (r *WorkflowRepository) loadSteps(ctx context.Context, definitionID string) ([]domain.WorkflowStep, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, definition_id, step_order, name, resolver_rule, condition, approval_mode, on_empty
		FROM workflow_steps WHERE definition_id = ? ORDER BY step_order`, definitionID)
	if err != nil {
		return nil, fmt.Errorf("load steps: %w", err)
	}
	defer rows.Close()

	var out []domain.WorkflowStep
	for rows.Next() {
		var s domain.WorkflowStep
		var ruleJSON string
		var conditionJSON sql.NullString

		if err := rows.Scan(&s.ID, &s.DefinitionID, &s.StepOrder, &s.Name, &ruleJSON, &conditionJSON, &s.ApprovalMode, &s.OnEmpty); err != nil {
			return nil, fmt.Errorf("scan step: %w", err)
		}
		if err := json.Unmarshal([]byte(ruleJSON), &s.ResolverRule); err != nil {
			return nil, fmt.Errorf("unmarshal resolver_rule for step %s: %w", s.ID, err)
		}
		if conditionJSON.Valid && conditionJSON.String != "" {
			var c domain.Condition
			if err := json.Unmarshal([]byte(conditionJSON.String), &c); err != nil {
				return nil, fmt.Errorf("unmarshal condition for step %s: %w", s.ID, err)
			}
			s.Condition = &c
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanDefinition(row rowScanner) (*domain.WorkflowDefinition, error) {
	var def domain.WorkflowDefinition
	var isActive int64
	if err := row.Scan(&def.ID, &def.AppID, &def.DocType, &def.Name, &def.Version, &isActive, &def.CreatedAt); err != nil {
		return nil, err
	}
	def.IsActive = isActive != 0
	return &def, nil
}
