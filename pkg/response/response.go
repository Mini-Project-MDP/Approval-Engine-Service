// Package response provides a consistent JSON envelope for API responses.
package response

import "github.com/gofiber/fiber/v2"

// Envelope is the shape of every JSON response this API returns. Exported
// (rather than the more common lowercase-private convention) specifically so
// swag can generate an accurate Swagger schema for it.
type Envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Success writes a 2xx JSON response with an optional payload.
func Success(c *fiber.Ctx, status int, message string, data any) error {
	return c.Status(status).JSON(Envelope{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// Error writes an error JSON response with the given status code and message.
func Error(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(Envelope{
		Success: false,
		Error:   message,
	})
}
