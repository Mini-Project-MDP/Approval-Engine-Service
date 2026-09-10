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

// PageMeta describes one page of a larger result set.
type PageMeta struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// Page is the data payload for a paginated list endpoint.
type Page struct {
	Items any      `json:"items"`
	Meta  PageMeta `json:"meta"`
}

// Paginated writes a 200 JSON response wrapping items with pagination meta.
func Paginated(c *fiber.Ctx, items any, page, limit, total int) error {
	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}
	return Success(c, fiber.StatusOK, "", Page{
		Items: items,
		Meta: PageMeta{
			Page:       page,
			Limit:      limit,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}
