package handler

import (
	"github.com/gofiber/fiber/v2"

	"approval-engine-service/pkg/response"
)

// HealthHandler exposes liveness/readiness endpoints.
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Check returns a simple 200 OK payload used by the client and infra probes.
//
// @Summary Health check
// @Tags health
// @Produce json
// @Success 200 {object} response.Envelope
// @Router /health [get]
func (h *HealthHandler) Check(c *fiber.Ctx) error {
	return response.Success(c, fiber.StatusOK, "service is healthy", fiber.Map{
		"status": "ok",
	})
}
