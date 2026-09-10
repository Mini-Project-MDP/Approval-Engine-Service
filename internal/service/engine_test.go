package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"approval-engine-service/internal/domain"
)

func newTestEngine() (*Engine, *fakeWorkflowRepo, *fakeRequestRepo, *fakeEventRepo) {
	workflows := newFakeWorkflowRepo()
	requests := newFakeRequestRepo()
	events := &fakeEventRepo{}
	participants := newFakeParticipants()
	resolver := NewResolver(participants)
	return NewEngine(workflows, requests, events, resolver, participants), workflows, requests, events
}

func step(order int, name string, rule domain.ResolverRule) domain.WorkflowStep {
	return domain.WorkflowStep{
		ID:           "step-" + name,
		StepOrder:    order,
		Name:         name,
		ResolverRule: rule,
		ApprovalMode: domain.ModeAny,
		OnEmpty:      domain.OnEmptyFail,
	}
}

func TestCreateRequestActivatesFirstStep(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{
			step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}),
			step(2, "regional_manager", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 2}),
		},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", map[string]any{"amount": float64(1000)})
	require.NoError(t, err)

	assert.Equal(t, domain.StatusPending, req.Status)
	assert.Equal(t, 1, req.CurrentStepOrder)
	require.Len(t, req.Steps, 1, "expected exactly the first step to be materialized")

	s := req.Steps[0]
	assert.Equal(t, domain.StepActive, s.Status)
	require.Len(t, s.Assignments, 1)
	assert.Equal(t, "SS01", s.Assignments[0].UserID)
}

func TestApprovalAdvancesThroughAllStepsToCompletion(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{
			step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}),
			step(2, "regional_manager", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 2}),
		},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", nil)
	require.NoError(t, err)

	req, err = engine.RecordDecision(context.Background(), req.ID, "SS01", domain.StatusApproved, nil)
	require.NoError(t, err, "approve step 1")
	require.Equal(t, domain.StatusPending, req.Status, "status after step 1 should still be pending")
	require.Equal(t, 2, req.CurrentStepOrder)
	require.Len(t, req.Steps, 2)
	require.Equal(t, "RSM1", req.Steps[1].Assignments[0].UserID, "expected step 2 active for RSM1")

	req, err = engine.RecordDecision(context.Background(), req.ID, "RSM1", domain.StatusApproved, nil)
	require.NoError(t, err, "approve step 2")
	assert.Equal(t, domain.StatusApproved, req.Status)
	assert.NotNil(t, req.CompletedAt)
}

func TestRejectionEndsRequestImmediately(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{
			step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}),
			step(2, "regional_manager", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 2}),
		},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", nil)
	require.NoError(t, err)

	req, err = engine.RecordDecision(context.Background(), req.ID, "SS01", domain.StatusRejected, nil)
	require.NoError(t, err, "reject")

	assert.Equal(t, domain.StatusRejected, req.Status)
	require.Len(t, req.Steps, 1, "step 2 must never be created after a rejection")
	assert.Equal(t, domain.StepRejected, req.Steps[0].Status)
}

func TestAllModeWaitsForEveryApprover(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	s := step(1, "finance_review", domain.ResolverRule{Type: domain.ResolverRole, Position: "Finance Reviewer"})
	s.ApprovalMode = domain.ModeAll
	workflows.register("assetmgmt", "expense", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "expense", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{s},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "expense", "EXP-1", "SA01", nil)
	require.NoError(t, err)
	require.Len(t, req.Steps[0].Assignments, 2, "expected both finance reviewers assigned")

	req, err = engine.RecordDecision(context.Background(), req.ID, "FR01", domain.StatusApproved, nil)
	require.NoError(t, err, "first approval")
	require.Equal(t, domain.StatusPending, req.Status, "should still be pending after only one of two approvals")
	require.Equal(t, domain.StepActive, req.Steps[0].Status)

	req, err = engine.RecordDecision(context.Background(), req.ID, "FR02", domain.StatusApproved, nil)
	require.NoError(t, err, "second approval")
	assert.Equal(t, domain.StatusApproved, req.Status)
}

func TestConditionSkipsStepWhenUnmet(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	gated := step(1, "director_review", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 4})
	gated.Condition = &domain.Condition{Field: "amount", Op: "gt", Value: float64(100)}
	always := step(2, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1})
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{gated, always},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", map[string]any{"amount": float64(50)})
	require.NoError(t, err)

	require.Len(t, req.Steps, 2, "expected the skipped step to still be recorded")
	assert.Equal(t, domain.StepSkipped, req.Steps[0].Status)
	require.Equal(t, domain.StepActive, req.Steps[1].Status)
	require.Len(t, req.Steps[1].Assignments, 1)
	assert.Equal(t, "SS01", req.Steps[1].Assignments[0].UserID)
}

func TestResolutionFailureParksTheRequest(t *testing.T) {
	engine, workflows, _, events := newTestEngine()
	tooHigh := step(1, "way_above_ceo", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 9})
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{tooHigh},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", nil)
	require.NoError(t, err, "create should not error just because a step can't resolve")

	assert.Equal(t, domain.StatusPending, req.Status, "expected pending (parked, not failed)")
	require.Len(t, req.Steps, 1)
	assert.Equal(t, domain.StepResolutionFailed, req.Steps[0].Status)

	_, err = engine.RecordDecision(context.Background(), req.ID, "SS01", domain.StatusApproved, nil)
	assert.Error(t, err, "there is no active step to decide on")

	eventTypes := make([]string, len(events.events))
	for i, e := range events.events {
		eventTypes[i] = e.EventType
	}
	assert.Contains(t, eventTypes, domain.EventResolutionFailed)
}

// TestVersionPinning is the guarantee the whole versioning design rests on:
// a request keeps running against the workflow version it started with, even
// after an admin publishes a new version of the same app+doc_type.
func TestVersionPinning(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	v1 := domain.WorkflowDefinition{
		ID: "def-v1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1})},
	}
	workflows.register("assetmgmt", "purchase_order", v1)

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", nil)
	require.NoError(t, err)
	require.Equal(t, "def-v1", req.DefinitionID, "expected the request pinned to def-v1")

	// Admin publishes v2 with an extra step for the same app+doc_type.
	v2 := domain.WorkflowDefinition{
		ID: "def-v2", AppID: "assetmgmt", DocType: "purchase_order", Version: 2, IsActive: true,
		Steps: []domain.WorkflowStep{
			step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1}),
			step(2, "extra_step_only_in_v2", domain.ResolverRule{Type: domain.ResolverStatic, UserID: "GDH01"}),
		},
	}
	workflows.register("assetmgmt", "purchase_order", v2)

	req, err = engine.RecordDecision(context.Background(), req.ID, "SS01", domain.StatusApproved, nil)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusApproved, req.Status, "the in-flight request must not pick up v2's extra step")
}

func TestCannotDecideTwiceOnTheSameAssignment(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1})},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", nil)
	require.NoError(t, err)

	_, err = engine.RecordDecision(context.Background(), req.ID, "SS01", domain.StatusApproved, nil)
	require.NoError(t, err, "first decision")

	_, err = engine.RecordDecision(context.Background(), req.ID, "SS01", domain.StatusApproved, nil)
	assert.Error(t, err, "the request is already approved")
}

func TestCreateRequestRejectsUnregisteredRequester(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1})},
	})

	// "GHOST01" does not exist in the fake participant directory: this is the
	// case where a consuming app's own login is buggy or compromised and
	// claims a requester we have never heard of.
	_, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "GHOST01", nil)
	assert.Error(t, err, "GHOST01 is not a registered participant")
}

func TestDecisionRejectsApproverWhoBecameInactiveAfterAssignment(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	fakeParts := newFakeParticipants()
	engine.participants = fakeParts
	engine.resolver = NewResolver(fakeParts)

	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1})},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", nil)
	require.NoError(t, err)

	// SS01 was active when assigned; simulate them resigning before acting.
	ss01 := fakeParts.byID["SS01"]
	ss01.IsActive = false
	fakeParts.byID["SS01"] = ss01

	_, err = engine.RecordDecision(context.Background(), req.ID, "SS01", domain.StatusApproved, nil)
	assert.Error(t, err, "SS01 is no longer an active participant")
}

func TestOnlyAssignedApproverCanDecide(t *testing.T) {
	engine, workflows, _, _ := newTestEngine()
	workflows.register("assetmgmt", "purchase_order", domain.WorkflowDefinition{
		ID: "def-1", AppID: "assetmgmt", DocType: "purchase_order", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{step(1, "supervisor", domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1})},
	})

	req, err := engine.CreateRequest(context.Background(), "assetmgmt", "purchase_order", "PO-1", "SA01", nil)
	require.NoError(t, err)

	_, err = engine.RecordDecision(context.Background(), req.ID, "RSM1", domain.StatusApproved, nil)
	assert.Error(t, err, "RSM1 is not the assigned approver on step 1")
}
