package domain

// Participant is a person who can raise or approve requests. Imported from
// FICOM and owned by this service, so approver resolution never depends on
// another system being reachable.
type Participant struct {
	UserID     string  `json:"user_id"`
	Name       string  `json:"name"`
	Email      string  `json:"email,omitempty"`
	Position   string  `json:"position,omitempty"`
	Department string  `json:"department,omitempty"`
	SuperiorID *string `json:"superior_id,omitempty"`
	IsActive   bool    `json:"is_active"`
	UpdatedAt  string  `json:"updated_at,omitempty"`
}
