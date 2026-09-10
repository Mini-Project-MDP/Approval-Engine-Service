package domain

// Application is a registered consumer of the engine (e.g. asset
// management's Spring Boot backend). APIKey is only ever returned once, at
// creation — callers must store it themselves.
type Application struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	APIKey    string `json:"api_key,omitempty"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at,omitempty"`
}
