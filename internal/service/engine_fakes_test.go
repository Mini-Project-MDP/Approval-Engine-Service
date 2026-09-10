package service

import (
	"context"

	"approval-engine-service/internal/domain"
)

// fakeWorkflowRepo lets tests point "active" at one definition while another
// (already used by an in-flight request) stays retrievable by id — exactly
// what proves version pinning.
type fakeWorkflowRepo struct {
	definitions map[string]domain.WorkflowDefinition
	active      map[string]string // "appID|docType" -> definition id
}

func newFakeWorkflowRepo() *fakeWorkflowRepo {
	return &fakeWorkflowRepo{
		definitions: map[string]domain.WorkflowDefinition{},
		active:      map[string]string{},
	}
}

func (f *fakeWorkflowRepo) register(appID, docType string, def domain.WorkflowDefinition) {
	f.definitions[def.ID] = def
	f.active[appID+"|"+docType] = def.ID
}

func (f *fakeWorkflowRepo) GetActiveDefinition(_ context.Context, appID, docType string) (*domain.WorkflowDefinition, error) {
	id, ok := f.active[appID+"|"+docType]
	if !ok {
		return nil, nil
	}
	def := f.definitions[id]
	return &def, nil
}

func (f *fakeWorkflowRepo) GetDefinitionByID(_ context.Context, id string) (*domain.WorkflowDefinition, error) {
	def, ok := f.definitions[id]
	if !ok {
		return nil, nil
	}
	return &def, nil
}

// fakeRequestRepo is an in-memory stand-in for the Turso-backed
// RequestRepository, faithful enough to exercise the engine's state machine.
type fakeRequestRepo struct {
	requests    map[string]*domain.ApprovalRequest
	steps       map[string]*domain.ApprovalStep
	stepOrder   map[string][]string // requestID -> step ids in creation order
	assignments map[string]*domain.ApprovalAssignment
}

func newFakeRequestRepo() *fakeRequestRepo {
	return &fakeRequestRepo{
		requests:    map[string]*domain.ApprovalRequest{},
		steps:       map[string]*domain.ApprovalStep{},
		stepOrder:   map[string][]string{},
		assignments: map[string]*domain.ApprovalAssignment{},
	}
}

func (f *fakeRequestRepo) CreateRequest(_ context.Context, req *domain.ApprovalRequest) error {
	cp := *req
	f.requests[req.ID] = &cp
	return nil
}

func (f *fakeRequestRepo) GetByID(_ context.Context, id string) (*domain.ApprovalRequest, error) {
	r, ok := f.requests[id]
	if !ok {
		return nil, nil
	}
	out := *r
	out.Steps = nil
	for _, sid := range f.stepOrder[id] {
		s := *f.steps[sid]
		s.Assignments = nil
		for _, a := range f.assignments {
			if a.StepID == sid {
				s.Assignments = append(s.Assignments, *a)
			}
		}
		out.Steps = append(out.Steps, s)
	}
	return &out, nil
}

func (f *fakeRequestRepo) UpdateRequestStatus(_ context.Context, id, status string, completedAt *string) error {
	f.requests[id].Status = status
	f.requests[id].CompletedAt = completedAt
	return nil
}

func (f *fakeRequestRepo) UpdateCurrentStep(_ context.Context, id string, stepOrder int) error {
	f.requests[id].CurrentStepOrder = stepOrder
	return nil
}

func (f *fakeRequestRepo) CreateStep(_ context.Context, step *domain.ApprovalStep) error {
	cp := *step
	f.steps[step.ID] = &cp
	f.stepOrder[step.RequestID] = append(f.stepOrder[step.RequestID], step.ID)
	return nil
}

func (f *fakeRequestRepo) UpdateStepStatus(_ context.Context, stepID, status string, completedAt *string) error {
	f.steps[stepID].Status = status
	f.steps[stepID].CompletedAt = completedAt
	return nil
}

func (f *fakeRequestRepo) CreateAssignment(_ context.Context, a *domain.ApprovalAssignment) error {
	cp := *a
	f.assignments[a.ID] = &cp
	return nil
}

func (f *fakeRequestRepo) GetAssignment(_ context.Context, stepID, userID string) (*domain.ApprovalAssignment, error) {
	for _, a := range f.assignments {
		if a.StepID == stepID && a.UserID == userID {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRequestRepo) ListAssignmentsByStep(_ context.Context, stepID string) ([]domain.ApprovalAssignment, error) {
	var out []domain.ApprovalAssignment
	for _, a := range f.assignments {
		if a.StepID == stepID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakeRequestRepo) UpdateAssignmentDecision(_ context.Context, id, status string, comment *string, actedAt string) error {
	a := f.assignments[id]
	a.Status = status
	a.Comment = comment
	a.ActedAt = &actedAt
	return nil
}

func (f *fakeRequestRepo) SkipPendingAssignments(_ context.Context, stepID string) error {
	for _, a := range f.assignments {
		if a.StepID == stepID && a.Status == domain.StatusPending {
			a.Status = "skipped"
		}
	}
	return nil
}

type fakeEventRepo struct {
	events []domain.ApprovalEvent
}

func (f *fakeEventRepo) Append(_ context.Context, e *domain.ApprovalEvent) error {
	f.events = append(f.events, *e)
	return nil
}
