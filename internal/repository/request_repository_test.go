package repository

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsUniqueConstraintErr checks the message-matching used to tell a real
// UNIQUE constraint violation (the losing side of a CreateRequest race — see
// domain.ErrDuplicateRequest) apart from any other insert failure. No DB
// needed: libsql-client-go doesn't expose a typed sentinel for this, so
// CreateRequest depends entirely on this string match being right.
func TestIsUniqueConstraintErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"sqlite-style message", errors.New("UNIQUE constraint failed: approval_requests.app_id, approval_requests.doc_type, approval_requests.resource_id"), true},
		{"lowercase variant", errors.New("unique constraint failed: applications.code"), true},
		{"unrelated db error", errors.New("no such table: approval_requests"), false},
		{"network error", errors.New("dial tcp: connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isUniqueConstraintErr(tt.err))
		})
	}
}
