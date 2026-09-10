package handler

import "github.com/gofiber/fiber/v2"

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// parsePagination reads page/limit query params, applying sane defaults and
// bounds so a caller can't request page 0 or an unbounded page size.
func parsePagination(c *fiber.Ctx) (page, limit int) {
	page = c.QueryInt("page", 1)
	if page < 1 {
		page = 1
	}
	limit = c.QueryInt("limit", defaultPageLimit)
	if limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return page, limit
}
