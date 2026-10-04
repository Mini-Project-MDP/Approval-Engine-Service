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

	// AdminAPIKey and PortalEnabled come straight from config (see
	// config.Config for what each one guards).
	AdminAPIKey   string
	PortalEnabled bool
}

// Register mounts all API routes under /api/v1.
//
// Routes are split by who is calling, because each kind of caller proves its
// identity differently:
//
//   - Application routes (/requests...): a consuming app's backend, proven by
//     X-API-Key. Handlers scope every read and decision to that app's own
//     requests. The app vouches for the user it names (it already
//     authenticated them); the engine still checks that user is the assigned
//     approver.
//   - Admin routes (/participants/import): operator tooling such as the FICOM
//     import, proven by X-Admin-Key. The org chart is shared by every app, so
//     no single app's key may rewrite it.
//   - Portal routes (/portal/...): the browser portal. It has no real user
//     login yet (the user is just the NIK it sends), so the whole group sits
//     behind PortalGate and can be switched off with PORTAL_ENABLED=false
//     until SSO login replaces that gate.
//   - Public routes: health, Swagger, and the QR/verify pair printed on
//     paper documents (verify is protected by its HMAC signature).
func Register(app *fiber.App, deps Dependencies) {
	app.Get("/swagger/*", fiberSwagger.HandlerDefault)

	api := app.Group("/api/v1")
	api.Get("/health", deps.Health.Check)

	api.Get("/assignments/:id/qr", deps.Signature.QR)
	api.Get("/verify/:id", deps.Signature.Verify)

	requests := api.Group("/requests", middleware.RequireAPIKey(deps.APIKeyLookup))
	requests.Post("/", deps.Requests.Create)
	requests.Get("/:id", deps.Requests.Get)
	requests.Post("/:id/decision", deps.Requests.Decide)

	api.Post("/participants/import", middleware.RequireAdminKey(deps.AdminAPIKey), deps.Participants.Import)

	portal := api.Group("/portal", middleware.PortalGate(deps.PortalEnabled))
	portal.Get("/inbox/:userID", deps.Inbox.List)
	portal.Get("/requests/:id", deps.Requests.Get)
	portal.Post("/requests/:id/decision", deps.Requests.Decide)

	applications := portal.Group("/applications")
	applications.Post("/", deps.Applications.Create)
	applications.Get("/", deps.Applications.List)

	workflows := portal.Group("/workflows")
	workflows.Post("/", deps.Workflows.Create)
	workflows.Get("/", deps.Workflows.List)
	workflows.Get("/:id", deps.Workflows.Get)
	workflows.Post("/:id/deactivate", deps.Workflows.Deactivate)

	portal.Get("/participants", deps.Participants.List)
}
