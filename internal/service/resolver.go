package service

import (
	"context"
	"fmt"

	"approval-engine-service/internal/domain"
)

// ParticipantStore is the slice of participant data the resolver needs.
// Declared here (not in the repository package) so the resolver can be tested
// without a database.
type ParticipantStore interface {
	GetByID(ctx context.Context, userID string) (*domain.Participant, error)
	FindByPosition(ctx context.Context, position, department string) ([]domain.Participant, error)
}

// Resolver turns a step's rule into the actual people who must approve.
//
// Every rule type ends in the same place — a list of participants that gets
// snapshotted onto the request — so supporting a new kind of rule later means
// adding one case here, not touching the engine or the schema.
type Resolver struct {
	participants ParticipantStore
}

func NewResolver(participants ParticipantStore) *Resolver {
	return &Resolver{participants: participants}
}

// Resolve returns the approvers for a rule. An empty result is not an error:
// the caller applies the step's on_empty policy (fail or skip).
func (r *Resolver) Resolve(ctx context.Context, rule domain.ResolverRule, req *domain.ApprovalRequest) ([]domain.Participant, error) {
	switch rule.Type {
	case domain.ResolverSuperior:
		return r.resolveSuperior(ctx, rule, req.RequesterID)
	case domain.ResolverRole:
		return r.resolveRole(ctx, rule, req.RequesterID)
	case domain.ResolverStatic:
		return r.resolveStatic(ctx, rule)
	case domain.ResolverField:
		return r.resolveField(ctx, rule, req.Payload)
	default:
		return nil, fmt.Errorf("unknown resolver type %q", rule.Type)
	}
}

// resolveSuperior walks up the superior chain. level 1 is the direct superior,
// level 2 the one above that, and so on — which is how "sales ++" and
// "operation ++" end up being the same rule with a different number.
func (r *Resolver) resolveSuperior(ctx context.Context, rule domain.ResolverRule, requesterID string) ([]domain.Participant, error) {
	if rule.Level < 1 {
		return nil, fmt.Errorf("superior rule needs level >= 1, got %d", rule.Level)
	}

	currentID := requesterID
	for i := 0; i < rule.Level; i++ {
		current, err := r.participants.GetByID(ctx, currentID)
		if err != nil {
			return nil, err
		}
		if current == nil || current.SuperiorID == nil || *current.SuperiorID == "" {
			// Chain is shorter than the workflow expects.
			return nil, nil
		}
		currentID = *current.SuperiorID
	}

	approver, err := r.participants.GetByID(ctx, currentID)
	if err != nil {
		return nil, err
	}
	if approver == nil || !approver.IsActive {
		return nil, nil
	}
	return []domain.Participant{*approver}, nil
}

// resolveRole finds people by position. An explicit Department is the
// cross-function hop (sales chain handing over to system support); scope
// "same_department" keeps it inside the requester's own department.
func (r *Resolver) resolveRole(ctx context.Context, rule domain.ResolverRule, requesterID string) ([]domain.Participant, error) {
	if rule.Position == "" {
		return nil, fmt.Errorf("role rule needs a position")
	}

	department := rule.Department
	if department == "" && rule.Scope == domain.ScopeSameDepartment {
		requester, err := r.participants.GetByID(ctx, requesterID)
		if err != nil {
			return nil, err
		}
		if requester == nil {
			return nil, nil
		}
		department = requester.Department
	}

	found, err := r.participants.FindByPosition(ctx, rule.Position, department)
	if err != nil {
		return nil, err
	}
	return activeOnly(found), nil
}

func (r *Resolver) resolveStatic(ctx context.Context, rule domain.ResolverRule) ([]domain.Participant, error) {
	if rule.UserID == "" {
		return nil, fmt.Errorf("static rule needs a user_id")
	}

	approver, err := r.participants.GetByID(ctx, rule.UserID)
	if err != nil {
		return nil, err
	}
	if approver == nil || !approver.IsActive {
		return nil, nil
	}
	return []domain.Participant{*approver}, nil
}

// resolveField reads approver ids straight from the request payload, for
// applications whose people are not in participants yet. Accepts one id or a
// list of them.
func (r *Resolver) resolveField(ctx context.Context, rule domain.ResolverRule, payload map[string]any) ([]domain.Participant, error) {
	if rule.Path == "" {
		return nil, fmt.Errorf("field rule needs a path")
	}

	raw, present := payload[rule.Path]
	if !present {
		return nil, nil
	}

	var ids []string
	switch v := raw.(type) {
	case string:
		if v != "" {
			ids = append(ids, v)
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				ids = append(ids, s)
			}
		}
	default:
		return nil, fmt.Errorf("payload field %q must be a string or list of strings", rule.Path)
	}

	var approvers []domain.Participant
	for _, id := range ids {
		p, err := r.participants.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if p == nil {
			// The app named someone this engine does not know. Keep the id so
			// the approval is still actionable and the gap is visible.
			approvers = append(approvers, domain.Participant{UserID: id, IsActive: true})
			continue
		}
		if p.IsActive {
			approvers = append(approvers, *p)
		}
	}
	return approvers, nil
}

func activeOnly(in []domain.Participant) []domain.Participant {
	var out []domain.Participant
	for _, p := range in {
		if p.IsActive {
			out = append(out, p)
		}
	}
	return out
}
