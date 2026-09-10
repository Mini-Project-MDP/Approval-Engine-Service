// Package router wires HTTP routes to their handlers.
package router

import (
	"github.com/gofiber/fiber/v2"
	fiberSwagger "github.com/gofiber/swagger"

	"approval-engine-service/internal/handler"
	"approval-engine-service/internal/middleware"
)

// Dependencies are the handlers/lookups the routes need. Built once in
// main.go and passed in, so router stays a pure wiring layer.
type Dependencies struct {
	Health       *handler.HealthHandler
	Requests     *handler.RequestHandler
	Inbox        *handler.InboxHandler
	Signature    *handler.SignatureHandler
	Applications *handler.ApplicationHandler
	Workflows    *handler.WorkflowHandler
	Participants *handler.ParticipantHandler
	APIKeyLookup middleware.ApplicationLookup
}

// Register mounts all API routes under /api/v1.
//
// Auth model (deliberately light for a 3-day MVP): creating a request is the
// one action a consuming application performs, so it alone requires the
// X-API-Key header. Everything else — reading a request, deciding on a step,
// the approver inbox, the QR/verify pair, and workflow/application
// management — is reached by the centralized portal or by anyone with a
// printed document's link, and relies on unguessable UUIDs plus the engine's
// own "are you the assigned approver" check rather than a full auth layer.
// Workflow/application management specifically has no role gate yet either —
// anyone who can reach the portal can reach it, an accepted gap for the demo
// rather than an oversight (a real deployment needs an is_admin check here).
func Register(app *fiber.App, deps Dependencies) {
	app.Get("/swagger/*", fiberSwagger.HandlerDefault)

	api := app.Group("/api/v1")
	api.Get("/health", deps.Health.Check)

	requests := api.Group("/requests")
	requests.Post("/", middleware.RequireAPIKey(deps.APIKeyLookup), deps.Requests.Create)
	requests.Get("/:id", deps.Requests.Get)
	requests.Post("/:id/decision", deps.Requests.Decide)

	api.Get("/inbox/:userID", deps.Inbox.List)

	api.Get("/assignments/:id/qr", deps.Signature.QR)
	api.Get("/verify/:id", deps.Signature.Verify)

	applications := api.Group("/applications")
	applications.Post("/", deps.Applications.Create)
	applications.Get("/", deps.Applications.List)

	workflows := api.Group("/workflows")
	workflows.Post("/", deps.Workflows.Create)
	workflows.Get("/", deps.Workflows.List)
	workflows.Get("/:id", deps.Workflows.Get)
	workflows.Post("/:id/deactivate", deps.Workflows.Deactivate)

	participants := api.Group("/participants")
	participants.Post("/import", deps.Participants.Import)
	participants.Get("/", deps.Participants.List)
}
