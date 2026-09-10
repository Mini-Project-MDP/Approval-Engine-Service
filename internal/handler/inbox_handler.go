package handler

import (
	"context"

	"github.com/gofiber/fiber/v2"

	"approval-engine-service/internal/repository"
	"approval-engine-service/pkg/response"
)

type InboxReader interface {
	ListInbox(ctx context.Context, userID string, page, limit int) ([]repository.InboxItem, int, error)
}

type InboxHandler struct {
	requests InboxReader
}

func NewInboxHandler(requests InboxReader) *InboxHandler {
	return &InboxHandler{requests: requests}
}

// List returns everything pending this approver's decision.
//
// @Summary Approver inbox
// @Description Everything pending this approver's decision, across every consuming application — the "one inbox for every system" promise of a centralized approval portal.
// @Tags inbox
// @Produce json
// @Param userID path string true "Approver NIK"
// @Param page query int false "Page number (default 1)"
// @Param limit query int false "Items per page (default 20, max 100)"
// @Success 200 {object} response.Envelope{data=response.Page}
// @Router /inbox/{userID} [get]
func (h *InboxHandler) List(c *fiber.Ctx) error {
	page, limit := parsePagination(c)
	items, total, err := h.requests.ListInbox(c.Context(), c.Params("userID"), page, limit)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Paginated(c, items, page, limit, total)
}
