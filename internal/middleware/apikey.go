package middleware

import (
	"context"

	"github.com/gofiber/fiber/v2"

	"approval-engine-service/pkg/response"
)

// ApplicationLookup resolves an API key to the calling application's id.
type ApplicationLookup interface {
	GetByAPIKey(ctx context.Context, apiKey string) (id string, isActive bool, err error)
}

// LocalsAppID is the fiber.Locals key RequireAPIKey stores the resolved
// application id under.
const LocalsAppID = "app_id"

// RequireAPIKey authenticates a consuming application via the X-API-Key
// header. Only endpoints that create data on behalf of a specific
// application need this — read-only/portal endpoints stay open (see
// router.Register for which is which).
func RequireAPIKey(lookup ApplicationLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := c.Get("X-API-Key")
		if key == "" {
			return response.Error(c, fiber.StatusUnauthorized, "missing X-API-Key header")
		}

		appID, isActive, err := lookup.GetByAPIKey(c.Context(), key)
		if err != nil {
			return response.Error(c, fiber.StatusInternalServerError, "failed to verify api key")
		}
		if appID == "" || !isActive {
			return response.Error(c, fiber.StatusUnauthorized, "invalid api key")
		}

		c.Locals(LocalsAppID, appID)
		return c.Next()
	}
}
