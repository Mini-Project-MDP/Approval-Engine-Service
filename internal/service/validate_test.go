package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"approval-engine-service/internal/domain"
)

func TestValidateStep(t *testing.T) {
	valid := domain.WorkflowStep{
		Name:         "Supervisor",
		ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1},
		ApprovalMode: domain.ModeAny,
		OnEmpty:      domain.OnEmptyFail,
	}
	assert.NoError(t, ValidateStep(valid), "expected a valid step to pass")

	tests := []struct {
		name string
		s    domain.WorkflowStep
	}{
		{"missing name", domain.WorkflowStep{ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail}},
		{"typo'd resolver type", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: "superviser"}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail}},
		{"superior with level 0", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 0}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail}},
		{"role without position", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverRole}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail}},
		{"static without user_id", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverStatic}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail}},
		{"field without path", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverField}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail}},
		{"bad approval_mode", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: "sometimes", OnEmpty: domain.OnEmptyFail}},
		{"bad on_empty", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: domain.ModeAny, OnEmpty: "retry"}},
		{"condition missing field", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail, Condition: &domain.Condition{Op: "gt", Value: 1}}},
		{"condition bad operator", domain.WorkflowStep{Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail, Condition: &domain.Condition{Field: "amount", Op: "between", Value: 1}}},
		{"condition and conditions both set", domain.WorkflowStep{
			Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail,
			Condition:  &domain.Condition{Field: "amount", Op: "gt", Value: 1},
			Conditions: []domain.Condition{{Field: "category", Op: "eq", Value: "barcode"}},
		}},
		{"conditions entry missing field", domain.WorkflowStep{
			Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail,
			Conditions: []domain.Condition{{Op: "gt", Value: 1}},
		}},
		{"conditions bad logic", domain.WorkflowStep{
			Name: "x", ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}, ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail,
			Conditions: []domain.Condition{{Field: "amount", Op: "gt", Value: 1}}, Logic: "xor",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, ValidateStep(tt.s))
		})
	}
}

func TestValidateStepAcceptsCompoundConditions(t *testing.T) {
	s := domain.WorkflowStep{
		Name:         "x",
		ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1},
		ApprovalMode: domain.ModeAny,
		OnEmpty:      domain.OnEmptyFail,
		Conditions: []domain.Condition{
			{Field: "amount", Op: "gt", Value: 50000000},
			{Field: "category", Op: "eq", Value: "barcode"},
		},
		Logic: domain.LogicAny,
	}
	assert.NoError(t, ValidateStep(s), "expected a well-formed compound condition to pass")
}
