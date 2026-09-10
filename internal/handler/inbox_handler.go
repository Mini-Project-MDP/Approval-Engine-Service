package handler

import (
	"context"

	"github.com/gofiber/fiber/v2"

	"approval-engine-service/internal/repository"
	"approval-engine-service/pkg/response"
)

type InboxReader interface {
	ListInbox(ctx context.Context, userID string) ([]repository.InboxItem, error)
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
// @Success 200 {object} response.Envelope{data=[]repository.InboxItem}
// @Router /inbox/{userID} [get]
func (h *InboxHandler) List(c *fiber.Ctx) error {
	items, err := h.requests.ListInbox(c.Context(), c.Params("userID"))
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, fiber.StatusOK, "", items)
}
