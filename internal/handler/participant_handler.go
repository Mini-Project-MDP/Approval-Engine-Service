package handler

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"

	"approval-engine-service/internal/domain"
	"approval-engine-service/pkg/response"
)

type ParticipantStore interface {
	UpsertBatch(ctx context.Context, participants []domain.Participant) error
	List(ctx context.Context) ([]domain.Participant, error)
}

// ParticipantHandler is the import surface for organizational data (from
// FICOM, per the team's decision to own a copy inside the engine rather than
// call out to FICOM at resolve time — see internal/service/resolver.go).
type ParticipantHandler struct {
	participants ParticipantStore
}

func NewParticipantHandler(participants ParticipantStore) *ParticipantHandler {
	return &ParticipantHandler{participants: participants}
}

// ImportBody is the payload for POST /participants/import.
type ImportBody struct {
	Participants []domain.Participant `json:"participants"`
}

// Import upserts a batch of participants.
//
// @Summary Import participants (e.g. from a FICOM export)
// @Description Upserts by user_id (NIK) in one transaction — a partial failure never leaves the table half-updated. Rows may reference a superior_id that appears later in the same array (or not at all yet); foreign keys are deferred to commit time, so the array does not need to be topologically sorted.
// @Tags participants
// @Accept json
// @Produce json
// @Param body body ImportBody true "Participants to import"
// @Success 200 {object} response.Envelope
// @Failure 400 {object} response.Envelope
// @Router /participants/import [post]
func (h *ParticipantHandler) Import(c *fiber.Ctx) error {
	var body ImportBody
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if len(body.Participants) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "participants must not be empty")
	}
	for i, p := range body.Participants {
		// user_id is the primary key: an empty one would either violate the
		// column's NOT NULL constraint at the DB layer with a confusing
		// error, or — worse — two blank entries in the same batch would
		// silently upsert over each other with no error at all.
		if p.UserID == "" {
			return response.Error(c, fiber.StatusBadRequest, fmt.Sprintf("participant %d: user_id is required", i+1))
		}
		if p.Name == "" {
			return response.Error(c, fiber.StatusBadRequest, fmt.Sprintf("participant %d (%s): name is required", i+1, p.UserID))
		}
	}

	if err := h.participants.UpsertBatch(c.Context(), body.Participants); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, fiber.StatusOK, "imported", fiber.Map{"count": len(body.Participants)})
}

// List returns every participant.
//
// @Summary List participants
// @Tags participants
// @Produce json
// @Success 200 {object} response.Envelope{data=[]domain.Participant}
// @Router /participants [get]
func (h *ParticipantHandler) List(c *fiber.Ctx) error {
	participants, err := h.participants.List(c.Context())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, fiber.StatusOK, "", participants)
}
