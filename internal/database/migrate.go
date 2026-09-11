package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sql
var schemaSQL string

// tables listed child-first so foreign keys never block a drop.
var tables = []string{
	"approval_events",
	"approval_assignments",
	"approval_steps",
	"approval_requests",
	"workflow_steps",
	"workflow_definitions",
	"participants",
	"applications",
}

// Reset drops every table. Development only: the schema is still changing
// daily, and CREATE TABLE IF NOT EXISTS cannot add a column to a table that
// already exists. Guarded by DB_RESET in main.
func Reset(db *sql.DB) error {
	for _, t := range tables {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + t); err != nil {
			return fmt.Errorf("drop %s: %w", t, err)
		}
	}
	return nil
}

// Migrate applies the schema. Every statement is CREATE ... IF NOT EXISTS,
// so this is safe to run on every startup.
func Migrate(db *sql.DB) error {
	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply schema (%.60s...): %w", stmt, err)
		}
	}
	// Columns added after a table already existed in production can't go
	// through CREATE TABLE IF NOT EXISTS above, so they're patched in here.
	if err := addColumnIfMissing(db, "applications", "callback_url", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "workflow_steps", "conditions", "TEXT"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "workflow_steps", "condition_logic", "TEXT"); err != nil {
		return err
	}
	return nil
}

// addColumnIfMissing runs an idempotent ALTER TABLE ADD COLUMN: harmless if
// the column is already there (fresh databases get it straight from
// schema.sql above), needed if it isn't (databases created before the
// column existed).
func addColumnIfMissing(db *sql.DB, table, column, ddlType string) error {
	_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, ddlType))
	if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// splitStatements drops "--" comment lines before splitting on ";", so that a
// semicolon inside a comment cannot cut a statement in half.
func splitStatements(script string) []string {
	var sb strings.Builder
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	var statements []string
	for _, stmt := range strings.Split(sb.String(), ";") {
		if strings.TrimSpace(stmt) != "" {
			statements = append(statements, stmt)
		}
	}
	return statements
}
