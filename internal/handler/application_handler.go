package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"approval-engine-service/internal/domain"
	"approval-engine-service/pkg/response"
)

type ApplicationStore interface {
	Create(ctx context.Context, app *domain.Application) error
	List(ctx context.Context, page, limit int) ([]domain.Application, int, error)
}

// ApplicationHandler manages consuming applications (e.g. asset
// management's Spring Boot backend) — the "X-API-Key" identities that can
// create requests. This is an internal/admin surface: it's reached only
// through the portal, never by a consuming app itself.
type ApplicationHandler struct {
	apps ApplicationStore
}

func NewApplicationHandler(apps ApplicationStore) *ApplicationHandler {
	return &ApplicationHandler{apps: apps}
}

// CreateApplicationBody is the payload for POST /applications.
type CreateApplicationBody struct {
	Code        string `json:"code" example:"assetmgmt"`
	Name        string `json:"name" example:"Asset Management"`
	CallbackURL string `json:"callback_url,omitempty" example:"https://assetmgmt.internal/api/approval-webhooks"`
}

// Create registers a new consuming application.
//
// @Summary Register a consuming application
// @Description Generates a new X-API-Key for the application. The key is only ever returned in this one response — List never includes it, so it must be copied to the consuming app's config immediately. callback_url is optional: if set, the engine POSTs a signed webhook there on every request/step state change (see WebhookEvent); if left empty the app can still always poll GET /requests/{id}. No role gate yet (accepted gap for the demo scope).
// @Tags applications
// @Accept json
// @Produce json
// @Param body body CreateApplicationBody true "Application to register"
// @Success 201 {object} response.Envelope{data=domain.Application} "includes api_key, shown once"
// @Failure 400 {object} response.Envelope "validation error or duplicate code"
// @Router /applications [post]
func (h *ApplicationHandler) Create(c *fiber.Ctx) error {
	var body CreateApplicationBody
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if body.Code == "" || body.Name == "" {
		return response.Error(c, fiber.StatusBadRequest, "code and name are required")
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to generate api key")
	}

	app := &domain.Application{
		ID: uuid.NewString(), Code: body.Code, Name: body.Name, APIKey: apiKey,
		CallbackURL: body.CallbackURL, IsActive: true,
	}
	if err := h.apps.Create(c.Context(), app); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, fiber.StatusCreated, "application created — copy the api_key now, it will not be shown again", app)
}

// List returns registered applications.
//
// @Summary List consuming applications
// @Description Never includes api_key — this is a read view for the management UI, not a way to recover a lost key.
// @Tags applications
// @Produce json
// @Param page query int false "Page number (default 1)"
// @Param limit query int false "Items per page (default 20, max 100)"
// @Success 200 {object} response.Envelope{data=response.Page}
// @Router /applications [get]
func (h *ApplicationHandler) List(c *fiber.Ctx) error {
	page, limit := parsePagination(c)
	apps, total, err := h.apps.List(c.Context(), page, limit)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	for i := range apps {
		apps[i].APIKey = ""
	}
	return response.Paginated(c, apps, page, limit, total)
}

func generateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
