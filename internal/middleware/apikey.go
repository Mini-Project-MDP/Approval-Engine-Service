package middleware

import (
	"context"
	"crypto/subtle"

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
// header. Every server-to-server route a consuming app calls sits behind
// this, and handlers use the resolved app id to keep each app scoped to its
// own requests (see router.Register for which routes are which).
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

// RequireAdminKey gates operator-only endpoints via the X-Admin-Key header,
// compared in constant time against the configured ADMIN_API_KEY. An empty
// configured key disables the endpoints entirely rather than leaving them
// open.
func RequireAdminKey(adminKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if adminKey == "" {
			return response.Error(c, fiber.StatusServiceUnavailable, "admin api is not configured")
		}
		given := c.Get("X-Admin-Key")
		if given == "" {
			return response.Error(c, fiber.StatusUnauthorized, "missing X-Admin-Key header")
		}
		if subtle.ConstantTimeCompare([]byte(given), []byte(adminKey)) != 1 {
			return response.Error(c, fiber.StatusUnauthorized, "invalid admin key")
		}
		return c.Next()
	}
}

// PortalGate switches the browser portal's routes on or off. Those routes
// identify the user only by the NIK the portal sends, so until SSO login is
// wired in, a locked-down deployment turns them off with PORTAL_ENABLED=false.
func PortalGate(enabled bool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !enabled {
			return response.Error(c, fiber.StatusForbidden, "portal is disabled on this deployment")
		}
		return c.Next()
	}
}
