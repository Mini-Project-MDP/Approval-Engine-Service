package service

import (
	"fmt"

	"approval-engine-service/internal/domain"
)

// ValidateStep catches a malformed workflow step (a typo in a resolver type,
// a missing field, an unknown condition operator) at publish time, in the
// admin UI, instead of letting it silently fail an approver's chain months
// later at resolve time.
func ValidateStep(s domain.WorkflowStep) error {
	if s.Name == "" {
		return fmt.Errorf("step name is required")
	}

	switch s.ResolverRule.Type {
	case domain.ResolverSuperior:
		if s.ResolverRule.Level < 1 {
			return fmt.Errorf("step %q: superior rule needs level >= 1", s.Name)
		}
	case domain.ResolverRole:
		if s.ResolverRule.Position == "" {
			return fmt.Errorf("step %q: role rule needs a position", s.Name)
		}
	case domain.ResolverStatic:
		if s.ResolverRule.UserID == "" {
			return fmt.Errorf("step %q: static rule needs a user_id", s.Name)
		}
	case domain.ResolverField:
		if s.ResolverRule.Path == "" {
			return fmt.Errorf("step %q: field rule needs a path", s.Name)
		}
	default:
		return fmt.Errorf("step %q: unknown resolver type %q", s.Name, s.ResolverRule.Type)
	}

	if s.ApprovalMode != domain.ModeAny && s.ApprovalMode != domain.ModeAll {
		return fmt.Errorf("step %q: approval_mode must be %q or %q", s.Name, domain.ModeAny, domain.ModeAll)
	}
	if s.OnEmpty != domain.OnEmptyFail && s.OnEmpty != domain.OnEmptySkip {
		return fmt.Errorf("step %q: on_empty must be %q or %q", s.Name, domain.OnEmptyFail, domain.OnEmptySkip)
	}

	if s.Condition != nil && len(s.Conditions) > 0 {
		return fmt.Errorf("step %q: set either condition or conditions, not both", s.Name)
	}

	if s.Condition != nil {
		if err := validateCondition(*s.Condition); err != nil {
			return fmt.Errorf("step %q: %w", s.Name, err)
		}
	}

	if len(s.Conditions) > 0 {
		switch s.Logic {
		case "", domain.LogicAll, domain.LogicAny:
		default:
			return fmt.Errorf("step %q: logic must be %q or %q", s.Name, domain.LogicAll, domain.LogicAny)
		}
		for _, c := range s.Conditions {
			if err := validateCondition(c); err != nil {
				return fmt.Errorf("step %q: %w", s.Name, err)
			}
		}
	}

	return nil
}

// validateCondition checks one Condition regardless of whether it came from
// the legacy singular Condition field or an entry in Conditions.
func validateCondition(c domain.Condition) error {
	if c.Field == "" {
		return fmt.Errorf("condition needs a field")
	}
	switch c.Op {
	case "eq", "ne", "gt", "gte", "lt", "lte", "in":
		return nil
	default:
		return fmt.Errorf("unknown condition operator %q", c.Op)
	}
}
