package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"approval-engine-service/internal/domain"
)

func TestEvaluateCondition(t *testing.T) {
	payload := map[string]any{
		"amount":   float64(75000000), // as it arrives from JSON
		"division": "sales",
		"urgent":   "true",
		"nik":      "00123", // leading zeros must not be numeric-matched away
	}

	tests := []struct {
		name string
		cond *domain.Condition
		want bool
	}{
		{"nil condition always runs", nil, true},
		{"amount above threshold", &domain.Condition{Field: "amount", Op: "gt", Value: float64(50000000)}, true},
		{"amount below threshold", &domain.Condition{Field: "amount", Op: "gt", Value: float64(100000000)}, false},
		{"gte at the boundary", &domain.Condition{Field: "amount", Op: "gte", Value: float64(75000000)}, true},
		{"lt", &domain.Condition{Field: "amount", Op: "lt", Value: float64(80000000)}, true},
		{"string equality", &domain.Condition{Field: "division", Op: "eq", Value: "sales"}, true},
		{"string inequality", &domain.Condition{Field: "division", Op: "ne", Value: "operation"}, true},
		{"in list", &domain.Condition{Field: "division", Op: "in", Value: []any{"sales", "marketing"}}, true},
		{"not in list", &domain.Condition{Field: "division", Op: "in", Value: []any{"operation"}}, false},
		{"missing field is unmet, not an error", &domain.Condition{Field: "nope", Op: "eq", Value: "x"}, false},
		{"number written as string still compares", &domain.Condition{Field: "amount", Op: "eq", Value: "75000000"}, true},
		{"threshold typed as string still compares", &domain.Condition{Field: "amount", Op: "gt", Value: "50000000"}, true},
		{"two strings stay strings", &domain.Condition{Field: "nik", Op: "eq", Value: "123"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvaluateCondition(tt.cond, payload)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEvaluateConditionRejectsUnknownOperator(t *testing.T) {
	_, err := EvaluateCondition(&domain.Condition{Field: "amount", Op: "between", Value: 1}, map[string]any{"amount": 5})
	assert.Error(t, err, "expected an error for an unknown operator")
}
