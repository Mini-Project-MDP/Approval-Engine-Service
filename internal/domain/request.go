package domain

// Request / step / assignment statuses.
const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusRejected  = "rejected"
	StatusCancelled = "cancelled"

	StepWaiting          = "waiting"
	StepActive           = "active"
	StepApproved         = "approved"
	StepRejected         = "rejected"
	StepSkipped          = "skipped"
	StepResolutionFailed = "resolution_failed"
)

// Audit event types.
const (
	EventRequestCreated   = "request_created"
	EventStepActivated    = "step_activated"
	EventStepSkipped      = "step_skipped"
	EventApproved         = "approved"
	EventRejected         = "rejected"
	EventRequestCompleted = "request_completed"
	EventResolutionFailed = "resolution_failed"
)

// ApprovalRequest is a running instance of a WorkflowDefinition.
type ApprovalRequest struct {
	ID               string         `json:"id"`
	AppID            string         `json:"app_id"`
	DefinitionID     string         `json:"definition_id"`
	DocType          string         `json:"doc_type"`
	ResourceID       string         `json:"resource_id"`
	RequesterID      string         `json:"requester_id"`
	Payload          map[string]any `json:"payload"`
	Status           string         `json:"status"`
	CurrentStepOrder int            `json:"current_step_order"`
	CreatedAt        string         `json:"created_at,omitempty"`
	CompletedAt      *string        `json:"completed_at,omitempty"`
	Steps            []ApprovalStep `json:"steps,omitempty"`
}

// ApprovalStep is a step materialised for one request.
type ApprovalStep struct {
	ID           string               `json:"id"`
	RequestID    string               `json:"request_id"`
	StepOrder    int                  `json:"step_order"`
	Name         string               `json:"name"`
	ApprovalMode string               `json:"approval_mode"`
	Status       string               `json:"status"`
	ActivatedAt  *string              `json:"activated_at,omitempty"`
	CompletedAt  *string              `json:"completed_at,omitempty"`
	Assignments  []ApprovalAssignment `json:"assignments,omitempty"`
}

// ApprovalAssignment records who was asked, snapshotted when the step became
// active. Name and position are copied on purpose: if someone changes role
// later, past approvals must still read the way they happened.
type ApprovalAssignment struct {
	ID           string  `json:"id"`
	StepID       string  `json:"step_id"`
	RequestID    string  `json:"request_id"`
	UserID       string  `json:"user_id"`
	UserName     string  `json:"user_name,omitempty"`
	UserPosition string  `json:"user_position,omitempty"`
	Status       string  `json:"status"`
	Comment      *string `json:"comment,omitempty"`
	ActedAt      *string `json:"acted_at,omitempty"`
}

// ApprovalEvent is one line of the append-only audit log.
type ApprovalEvent struct {
	ID        string         `json:"id"`
	RequestID string         `json:"request_id"`
	StepID    *string        `json:"step_id,omitempty"`
	ActorID   *string        `json:"actor_id,omitempty"`
	EventType string         `json:"event_type"`
	Detail    map[string]any `json:"detail,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
}
