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
	List(ctx context.Context) ([]domain.Application, error)
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
	Code string `json:"code" example:"assetmgmt"`
	Name string `json:"name" example:"Asset Management"`
}

// Create registers a new consuming application.
//
// @Summary Register a consuming application
// @Description Generates a new X-API-Key for the application. The key is only ever returned in this one response — List never includes it, so it must be copied to the consuming app's config immediately. No role gate yet (accepted gap for the demo scope).
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
		ID: uuid.NewString(), Code: body.Code, Name: body.Name, APIKey: apiKey, IsActive: true,
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
// @Success 200 {object} response.Envelope{data=[]domain.Application}
// @Router /applications [get]
func (h *ApplicationHandler) List(c *fiber.Ctx) error {
	apps, err := h.apps.List(c.Context())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	for i := range apps {
		apps[i].APIKey = ""
	}
	return response.Success(c, fiber.StatusOK, "", apps)
}

func generateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
