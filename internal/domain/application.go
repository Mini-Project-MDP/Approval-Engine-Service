package domain

// Application is a registered consumer of the engine (e.g. asset
// management's Spring Boot backend). APIKey is only ever returned once, at
// creation — callers must store it themselves.
//
// CallbackURL, if set, is where the engine POSTs a webhook notification
// whenever a request/step belonging to this app changes state — see
// service.WebhookNotifier. It's optional: an app that leaves it empty just
// keeps polling GET /requests/{id} as before.
type Application struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	APIKey      string `json:"api_key,omitempty"`
	CallbackURL string `json:"callback_url,omitempty"`
	IsActive    bool   `json:"is_active"`
	CreatedAt   string `json:"created_at,omitempty"`
}
