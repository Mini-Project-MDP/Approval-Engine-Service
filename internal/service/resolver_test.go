package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"approval-engine-service/internal/domain"
)

// fakeParticipants models the cross-function chain the engine has to support:
// Sales Admin -> SS -> RSM -> NSM -> SD, then a hop into System Support.
type fakeParticipants struct {
	byID map[string]domain.Participant
}

func newFakeParticipants() *fakeParticipants {
	ptr := func(s string) *string { return &s }
	return &fakeParticipants{byID: map[string]domain.Participant{
		"SA01":  {UserID: "SA01", Name: "Sales Admin", Position: "Sales Admin", Department: "Sales", SuperiorID: ptr("SS01"), IsActive: true},
		"SS01":  {UserID: "SS01", Name: "Supervisor", Position: "SS", Department: "Sales", SuperiorID: ptr("RSM1"), IsActive: true},
		"RSM1":  {UserID: "RSM1", Name: "Regional Mgr", Position: "RSM", Department: "Sales", SuperiorID: ptr("NSM1"), IsActive: true},
		"NSM1":  {UserID: "NSM1", Name: "National Mgr", Position: "NSM", Department: "Sales", SuperiorID: ptr("SD01"), IsActive: true},
		"SD01":  {UserID: "SD01", Name: "Sales Director", Position: "SD", Department: "Sales", IsActive: true},
		"AST01": {UserID: "AST01", Name: "Asset Officer", Position: "Asset Officer", Department: "System Support", IsActive: true},
		"GDH01": {UserID: "GDH01", Name: "GDH", Position: "GDH", Department: "System Support", IsActive: true},
		"OLD01": {UserID: "OLD01", Name: "Resigned", Position: "GDH", Department: "System Support", IsActive: false},
		"FR01":  {UserID: "FR01", Name: "Finance Reviewer A", Position: "Finance Reviewer", Department: "Finance", IsActive: true},
		"FR02":  {UserID: "FR02", Name: "Finance Reviewer B", Position: "Finance Reviewer", Department: "Finance", IsActive: true},
	}}
}

func (f *fakeParticipants) GetByID(_ context.Context, userID string) (*domain.Participant, error) {
	p, ok := f.byID[userID]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (f *fakeParticipants) FindByPosition(_ context.Context, position, department string) ([]domain.Participant, error) {
	var out []domain.Participant
	for _, p := range f.byID {
		if p.Position != position {
			continue
		}
		if department != "" && p.Department != department {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func TestResolveSuperiorWalksTheChain(t *testing.T) {
	r := NewResolver(newFakeParticipants())
	req := &domain.ApprovalRequest{RequesterID: "SA01"}

	cases := map[int]string{1: "SS01", 2: "RSM1", 3: "NSM1", 4: "SD01"}
	for level, wantID := range cases {
		got, err := r.Resolve(context.Background(), domain.ResolverRule{Type: domain.ResolverSuperior, Level: level}, req)
		require.NoErrorf(t, err, "level %d", level)
		require.Lenf(t, got, 1, "level %d", level)
		require.Equalf(t, wantID, got[0].UserID, "level %d", level)
	}
}

func TestResolveSuperiorBeyondChainReturnsEmpty(t *testing.T) {
	r := NewResolver(newFakeParticipants())
	req := &domain.ApprovalRequest{RequesterID: "SA01"}

	got, err := r.Resolve(context.Background(), domain.ResolverRule{Type: domain.ResolverSuperior, Level: 9}, req)
	require.NoError(t, err)
	require.Empty(t, got, "expected nobody when the chain runs out")
}

func TestResolveRoleCrossFunction(t *testing.T) {
	r := NewResolver(newFakeParticipants())
	req := &domain.ApprovalRequest{RequesterID: "SA01"} // requester is in Sales

	got, err := r.Resolve(context.Background(), domain.ResolverRule{
		Type:       domain.ResolverRole,
		Position:   "Asset Officer",
		Department: "System Support",
	}, req)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "AST01", got[0].UserID, "expected the System Support asset officer")
}

func TestResolveRoleSkipsInactivePeople(t *testing.T) {
	r := NewResolver(newFakeParticipants())
	req := &domain.ApprovalRequest{RequesterID: "SA01"}

	got, err := r.Resolve(context.Background(), domain.ResolverRule{
		Type:       domain.ResolverRole,
		Position:   "GDH",
		Department: "System Support",
	}, req)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "GDH01", got[0].UserID, "expected only the active GDH")
}

func TestResolveRoleSameDepartment(t *testing.T) {
	r := NewResolver(newFakeParticipants())
	req := &domain.ApprovalRequest{RequesterID: "SA01"}

	got, err := r.Resolve(context.Background(), domain.ResolverRule{
		Type:     domain.ResolverRole,
		Position: "RSM",
		Scope:    domain.ScopeSameDepartment,
	}, req)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "RSM1", got[0].UserID, "expected the Sales RSM")
}

func TestResolveFieldFromPayload(t *testing.T) {
	r := NewResolver(newFakeParticipants())
	req := &domain.ApprovalRequest{
		RequesterID: "SA01",
		Payload:     map[string]any{"approver_nik": "SD01"},
	}

	got, err := r.Resolve(context.Background(), domain.ResolverRule{Type: domain.ResolverField, Path: "approver_nik"}, req)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "SD01", got[0].UserID)
}

func TestResolveStatic(t *testing.T) {
	r := NewResolver(newFakeParticipants())
	req := &domain.ApprovalRequest{RequesterID: "SA01"}

	got, err := r.Resolve(context.Background(), domain.ResolverRule{Type: domain.ResolverStatic, UserID: "GDH01"}, req)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "GDH01", got[0].UserID)
}
