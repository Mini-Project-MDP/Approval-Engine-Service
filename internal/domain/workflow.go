package domain

// Resolver rule types.
const (
	ResolverSuperior = "superior" // walk N steps up the requester's superior chain
	ResolverRole     = "role"     // find people by position, optionally in another department
	ResolverStatic   = "static"   // a fixed person
	ResolverField    = "field"    // read the approver id out of the request payload
)

// Approval modes.
const (
	ModeAny = "any" // one approver is enough
	ModeAll = "all" // everyone resolved must approve
)

// What to do when a rule resolves to nobody.
const (
	OnEmptyFail = "fail"
	OnEmptySkip = "skip"
)

// ScopeSameDepartment restricts a role rule to the requester's own department.
const ScopeSameDepartment = "same_department"

// Combine logic for a step's Conditions list. Only meaningful when Conditions
// is used instead of the single legacy Condition field.
const (
	LogicAll = "all" // every condition must pass (default when omitted)
	LogicAny = "any" // at least one condition must pass
)

// ResolverRule says how to find the approver(s) for a step. Stored as JSON so
// system-support users can add new flows without a code change.
type ResolverRule struct {
	Type string `json:"type"`

	Level int `json:"level,omitempty"` // superior

	Position   string `json:"position,omitempty"`   // role
	Department string `json:"department,omitempty"` // role: explicit = cross-function hop
	Scope      string `json:"scope,omitempty"`      // role: "same_department"

	UserID string `json:"user_id,omitempty"` // static
	Path   string `json:"path,omitempty"`    // field: key inside the request payload
}

// Condition gates whether a step runs, evaluated against the request payload.
// A nil condition means the step always runs.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq, ne, gt, gte, lt, lte, in
	Value any    `json:"value"`
}

// WorkflowDefinition is the blueprint for one application + document type.
// Versioned so in-flight requests keep the rules they started with.
type WorkflowDefinition struct {
	ID        string         `json:"id"`
	AppID     string         `json:"app_id"`
	DocType   string         `json:"doc_type"`
	Name      string         `json:"name"`
	Version   int            `json:"version"`
	IsActive  bool           `json:"is_active"`
	CreatedAt string         `json:"created_at,omitempty"`
	Steps     []WorkflowStep `json:"steps,omitempty"`
}

// WorkflowStep is one stage of a blueprint.
//
// A step gates on at most one of Condition or Conditions — never both (see
// ValidateStep). Condition is the original single-condition shape and stays
// exactly as it behaved before; Conditions+Logic is the compound form for a
// step that needs to test more than one payload field (e.g. amount AND
// category) without a consumer having to fork into multiple doc_types to
// fake an AND. EvaluateStepConditions is what actually decides whether a
// step runs — it checks Conditions first, falling back to Condition.
type WorkflowStep struct {
	ID           string       `json:"id"`
	DefinitionID string       `json:"definition_id"`
	StepOrder    int          `json:"step_order"`
	Name         string       `json:"name"`
	ResolverRule ResolverRule `json:"resolver_rule"`
	Condition    *Condition   `json:"condition,omitempty"`
	Conditions   []Condition  `json:"conditions,omitempty"`
	Logic        string       `json:"logic,omitempty"` // LogicAll (default) or LogicAny; only used with Conditions
	ApprovalMode string       `json:"approval_mode"`
	OnEmpty      string       `json:"on_empty"`
}
