package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"approval-engine-service/internal/domain"
)

// Ports the engine depends on. Repository implementations satisfy these;
// tests use fakes, so the state machine can be verified without a database.

type WorkflowRepository interface {
	GetActiveDefinition(ctx context.Context, appID, docType string) (*domain.WorkflowDefinition, error)
	GetDefinitionByID(ctx context.Context, id string) (*domain.WorkflowDefinition, error)
}

type RequestRepository interface {
	CreateRequest(ctx context.Context, req *domain.ApprovalRequest) error
	GetByID(ctx context.Context, id string) (*domain.ApprovalRequest, error)
	UpdateRequestStatus(ctx context.Context, id, status string, completedAt *string) error
	UpdateCurrentStep(ctx context.Context, id string, stepOrder int) error

	CreateStep(ctx context.Context, step *domain.ApprovalStep) error
	UpdateStepStatus(ctx context.Context, stepID, status string, completedAt *string) error

	CreateAssignment(ctx context.Context, a *domain.ApprovalAssignment) error
	GetAssignment(ctx context.Context, stepID, userID string) (*domain.ApprovalAssignment, error)
	ListAssignmentsByStep(ctx context.Context, stepID string) ([]domain.ApprovalAssignment, error)
	UpdateAssignmentDecision(ctx context.Context, id, status string, comment *string, actedAt string) error
	SkipPendingAssignments(ctx context.Context, stepID string) error
}

type EventRepository interface {
	Append(ctx context.Context, e *domain.ApprovalEvent) error
}

// Engine runs the approval state machine: materialize the right step,
// resolve who must act on it, record decisions, and advance or close the
// request — writing an audit event at every transition.
//
// Callers (a consuming app's backend, authenticated by its own API key) are
// trusted about WHICH application is calling, but the person they claim is
// acting is never trusted blindly: every requester_id and every decision's
// user_id is checked against participants, so a bug (or a compromised key)
// on the consumer side can't create or decide requests as someone who isn't
// a real, active, registered person.
type Engine struct {
	workflows    WorkflowRepository
	requests     RequestRepository
	events       EventRepository
	resolver     *Resolver
	participants ParticipantStore
	notifier     *WebhookNotifier
}

// notifier may be nil (tests, or a caller that doesn't need webhooks) —
// logEvent skips delivery entirely in that case.
func NewEngine(workflows WorkflowRepository, requests RequestRepository, events EventRepository, resolver *Resolver, participants ParticipantStore, notifier *WebhookNotifier) *Engine {
	return &Engine{workflows: workflows, requests: requests, events: events, resolver: resolver, participants: participants, notifier: notifier}
}

// CreateRequest starts a new approval flow against the app's currently
// active workflow definition and immediately activates (or skips through)
// leading steps until an approver is found or the request completes.
func (e *Engine) CreateRequest(ctx context.Context, appID, docType, resourceID, requesterID string, payload map[string]any) (*domain.ApprovalRequest, error) {
	requester, err := e.participants.GetByID(ctx, requesterID)
	if err != nil {
		return nil, fmt.Errorf("look up requester: %w", err)
	}
	if requester == nil {
		return nil, fmt.Errorf("requester %q is not a registered participant", requesterID)
	}
	if !requester.IsActive {
		return nil, fmt.Errorf("requester %q is registered but not active", requesterID)
	}

	def, err := e.workflows.GetActiveDefinition(ctx, appID, docType)
	if err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}
	if def == nil {
		return nil, fmt.Errorf("no active workflow for app %q doc_type %q", appID, docType)
	}
	if len(def.Steps) == 0 {
		return nil, fmt.Errorf("workflow %q has no steps", def.ID)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	req := &domain.ApprovalRequest{
		ID:           uuid.NewString(),
		AppID:        appID,
		DefinitionID: def.ID,
		DocType:      docType,
		ResourceID:   resourceID,
		RequesterID:  requesterID,
		Payload:      payload,
		Status:       domain.StatusPending,
	}
	if err := e.requests.CreateRequest(ctx, req); err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if err := e.logEvent(ctx, appID, req.ID, nil, nil, domain.EventRequestCreated, map[string]any{
		"doc_type": docType, "resource_id": resourceID,
	}); err != nil {
		return nil, err
	}

	if err := e.advance(ctx, req, def); err != nil {
		return nil, err
	}
	return e.requests.GetByID(ctx, req.ID)
}

// RecordDecision applies one approver's decision on the currently active
// step, closes the step out if the mode (any/all) is satisfied, and advances
// the workflow using the exact definition version the request started on.
func (e *Engine) RecordDecision(ctx context.Context, requestID, userID, decision string, comment *string) (*domain.ApprovalRequest, error) {
	if decision != domain.StatusApproved && decision != domain.StatusRejected {
		return nil, fmt.Errorf("decision must be %q or %q, got %q", domain.StatusApproved, domain.StatusRejected, decision)
	}

	req, err := e.requests.GetByID(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("load request: %w", err)
	}
	if req == nil {
		return nil, fmt.Errorf("request %q not found", requestID)
	}
	if req.Status != domain.StatusPending {
		return nil, fmt.Errorf("request %q is already %s", requestID, req.Status)
	}

	activeStep := findActiveStep(req.Steps)
	if activeStep == nil {
		return nil, fmt.Errorf("request %q has no active step awaiting a decision", requestID)
	}

	assignment, err := e.requests.GetAssignment(ctx, activeStep.ID, userID)
	if err != nil {
		return nil, fmt.Errorf("load assignment: %w", err)
	}
	if assignment == nil {
		return nil, fmt.Errorf("user %q is not an approver on this step", userID)
	}
	if assignment.Status != domain.StatusPending {
		return nil, fmt.Errorf("user %q already acted on this step", userID)
	}

	actor, err := e.participants.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("look up actor: %w", err)
	}
	if actor == nil || !actor.IsActive {
		// Assigned when the step activated, but no longer a valid person to
		// act (e.g. resigned mid-flight). A human needs to reassign this step
		// rather than let a stale identity decide it.
		return nil, fmt.Errorf("user %q is no longer an active participant and cannot decide", userID)
	}

	now := nowString()
	if err := e.requests.UpdateAssignmentDecision(ctx, assignment.ID, decision, comment, now); err != nil {
		return nil, fmt.Errorf("record decision: %w", err)
	}

	eventType := domain.EventApproved
	if decision == domain.StatusRejected {
		eventType = domain.EventRejected
	}
	actorID := userID
	if err := e.logEvent(ctx, req.AppID, requestID, &activeStep.ID, &actorID, eventType, map[string]any{"step": activeStep.Name}); err != nil {
		return nil, err
	}

	if decision == domain.StatusRejected {
		if err := e.requests.UpdateStepStatus(ctx, activeStep.ID, domain.StepRejected, &now); err != nil {
			return nil, fmt.Errorf("close rejected step: %w", err)
		}
		if err := e.requests.SkipPendingAssignments(ctx, activeStep.ID); err != nil {
			return nil, fmt.Errorf("skip remaining approvers: %w", err)
		}
		if err := e.completeRequest(ctx, req.AppID, req.ID, domain.StatusRejected); err != nil {
			return nil, err
		}
		return e.requests.GetByID(ctx, requestID)
	}

	satisfied, err := e.stepSatisfied(ctx, activeStep)
	if err != nil {
		return nil, err
	}
	if !satisfied {
		// "all" mode, other approvers still pending.
		return e.requests.GetByID(ctx, requestID)
	}

	if err := e.requests.UpdateStepStatus(ctx, activeStep.ID, domain.StepApproved, &now); err != nil {
		return nil, fmt.Errorf("close approved step: %w", err)
	}
	if err := e.requests.SkipPendingAssignments(ctx, activeStep.ID); err != nil {
		return nil, fmt.Errorf("skip remaining approvers: %w", err)
	}

	def, err := e.workflows.GetDefinitionByID(ctx, req.DefinitionID)
	if err != nil {
		return nil, fmt.Errorf("load workflow version %s: %w", req.DefinitionID, err)
	}
	if def == nil {
		return nil, fmt.Errorf("workflow version %s no longer exists", req.DefinitionID)
	}

	req.CurrentStepOrder = activeStep.StepOrder
	if err := e.advance(ctx, req, def); err != nil {
		return nil, err
	}
	return e.requests.GetByID(ctx, requestID)
}

// advance walks the definition's steps after req.CurrentStepOrder, skipping
// any whose condition is unmet, until it activates a step with at least one
// approver, hits a step that fails to resolve (on_empty=fail parks the
// request), or runs out of steps (the request is then complete).
func (e *Engine) advance(ctx context.Context, req *domain.ApprovalRequest, def *domain.WorkflowDefinition) error {
	for _, step := range def.Steps {
		if step.StepOrder <= req.CurrentStepOrder {
			continue
		}

		runs, err := EvaluateCondition(step.Condition, req.Payload)
		if err != nil {
			return fmt.Errorf("step %d condition: %w", step.StepOrder, err)
		}
		if !runs {
			if err := e.materializeSkippedStep(ctx, req.AppID, req.ID, step); err != nil {
				return err
			}
			req.CurrentStepOrder = step.StepOrder
			continue
		}

		approvers, err := e.resolver.Resolve(ctx, step.ResolverRule, req)
		if err != nil {
			return fmt.Errorf("step %d resolve: %w", step.StepOrder, err)
		}

		if len(approvers) == 0 {
			if step.OnEmpty == domain.OnEmptySkip {
				if err := e.materializeSkippedStep(ctx, req.AppID, req.ID, step); err != nil {
					return err
				}
				req.CurrentStepOrder = step.StepOrder
				continue
			}
			return e.parkOnResolutionFailure(ctx, req.AppID, req.ID, step)
		}

		return e.activateStep(ctx, req.AppID, req.ID, step, approvers)
	}

	return e.completeRequest(ctx, req.AppID, req.ID, domain.StatusApproved)
}

func (e *Engine) activateStep(ctx context.Context, appID, requestID string, step domain.WorkflowStep, approvers []domain.Participant) error {
	now := nowString()
	s := &domain.ApprovalStep{
		ID:           uuid.NewString(),
		RequestID:    requestID,
		StepOrder:    step.StepOrder,
		Name:         step.Name,
		ApprovalMode: step.ApprovalMode,
		Status:       domain.StepActive,
		ActivatedAt:  &now,
	}
	if err := e.requests.CreateStep(ctx, s); err != nil {
		return fmt.Errorf("create step: %w", err)
	}

	approverIDs := make([]string, 0, len(approvers))
	for _, p := range approvers {
		a := &domain.ApprovalAssignment{
			ID:           uuid.NewString(),
			StepID:       s.ID,
			RequestID:    requestID,
			UserID:       p.UserID,
			UserName:     p.Name,
			UserPosition: p.Position,
			Status:       domain.StatusPending,
		}
		if err := e.requests.CreateAssignment(ctx, a); err != nil {
			return fmt.Errorf("create assignment for %s: %w", p.UserID, err)
		}
		approverIDs = append(approverIDs, p.UserID)
	}

	if err := e.requests.UpdateCurrentStep(ctx, requestID, step.StepOrder); err != nil {
		return fmt.Errorf("advance current step: %w", err)
	}
	return e.logEvent(ctx, appID, requestID, &s.ID, nil, domain.EventStepActivated, map[string]any{
		"step": step.Name, "approvers": approverIDs, "mode": step.ApprovalMode,
	})
}

func (e *Engine) materializeSkippedStep(ctx context.Context, appID, requestID string, step domain.WorkflowStep) error {
	now := nowString()
	s := &domain.ApprovalStep{
		ID:           uuid.NewString(),
		RequestID:    requestID,
		StepOrder:    step.StepOrder,
		Name:         step.Name,
		ApprovalMode: step.ApprovalMode,
		Status:       domain.StepSkipped,
		ActivatedAt:  &now,
		CompletedAt:  &now,
	}
	if err := e.requests.CreateStep(ctx, s); err != nil {
		return fmt.Errorf("create skipped step: %w", err)
	}
	if err := e.requests.UpdateCurrentStep(ctx, requestID, step.StepOrder); err != nil {
		return fmt.Errorf("advance current step: %w", err)
	}
	return e.logEvent(ctx, appID, requestID, &s.ID, nil, domain.EventStepSkipped, map[string]any{"step": step.Name})
}

// parkOnResolutionFailure leaves the request pending but with no active
// assignment: a rule resolved to nobody (chain too short, no one holds the
// role) and on_empty=fail. Nothing here auto-retries; a human resolves the
// data gap and the step is re-run. Deliberately not built further than this
// for now (no scheduler, no notifications) to keep v1 shippable.
func (e *Engine) parkOnResolutionFailure(ctx context.Context, appID, requestID string, step domain.WorkflowStep) error {
	s := &domain.ApprovalStep{
		ID:           uuid.NewString(),
		RequestID:    requestID,
		StepOrder:    step.StepOrder,
		Name:         step.Name,
		ApprovalMode: step.ApprovalMode,
		Status:       domain.StepResolutionFailed,
	}
	if err := e.requests.CreateStep(ctx, s); err != nil {
		return fmt.Errorf("create failed step: %w", err)
	}
	return e.logEvent(ctx, appID, requestID, &s.ID, nil, domain.EventResolutionFailed, map[string]any{
		"step": step.Name, "rule": step.ResolverRule,
	})
}

func (e *Engine) completeRequest(ctx context.Context, appID, requestID, status string) error {
	now := nowString()
	if err := e.requests.UpdateRequestStatus(ctx, requestID, status, &now); err != nil {
		return fmt.Errorf("complete request: %w", err)
	}
	return e.logEvent(ctx, appID, requestID, nil, nil, domain.EventRequestCompleted, map[string]any{"outcome": status})
}

func (e *Engine) stepSatisfied(ctx context.Context, step *domain.ApprovalStep) (bool, error) {
	assignments, err := e.requests.ListAssignmentsByStep(ctx, step.ID)
	if err != nil {
		return false, fmt.Errorf("list assignments: %w", err)
	}

	if step.ApprovalMode == domain.ModeAll {
		for _, a := range assignments {
			if a.Status != domain.StatusApproved {
				return false, nil
			}
		}
		return true, nil
	}

	for _, a := range assignments {
		if a.Status == domain.StatusApproved {
			return true, nil
		}
	}
	return false, nil
}

// logEvent writes the append-only audit record for a transition and, in the
// same breath, hands the equivalent WebhookEvent to the notifier — every
// state change a consuming app can see via GET /requests/{id} also reaches
// it (best-effort) as a push. notifier is nil-safe (Notify no-ops), so
// callers that don't need webhooks (tests, smoketest) can pass nil.
func (e *Engine) logEvent(ctx context.Context, appID, requestID string, stepID, actorID *string, eventType string, detail map[string]any) error {
	occurredAt := nowString()
	if err := e.events.Append(ctx, &domain.ApprovalEvent{
		ID:        uuid.NewString(),
		RequestID: requestID,
		StepID:    stepID,
		ActorID:   actorID,
		EventType: eventType,
		Detail:    detail,
	}); err != nil {
		return fmt.Errorf("append audit event %s: %w", eventType, err)
	}
	e.notifier.Notify(appID, WebhookEvent{
		Event:      eventType,
		AppID:      appID,
		RequestID:  requestID,
		StepID:     stepID,
		ActorID:    actorID,
		Detail:     detail,
		OccurredAt: occurredAt,
	})
	return nil
}

func findActiveStep(steps []domain.ApprovalStep) *domain.ApprovalStep {
	for i := range steps {
		if steps[i].Status == domain.StepActive {
			return &steps[i]
		}
	}
	return nil
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}
