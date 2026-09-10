package handler

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	qrcode "github.com/skip2/go-qrcode"

	"approval-engine-service/internal/domain"
	"approval-engine-service/internal/service"
	"approval-engine-service/pkg/response"
)

type AssignmentReader interface {
	GetAssignmentByID(ctx context.Context, id string) (*domain.ApprovalAssignment, error)
}

// SignatureHandler is the paper-trail bridge: it hands the consuming app a
// QR code image to print as an approver's "signature" stamp, and exposes a
// public endpoint anyone can scan to confirm the stamp is genuine. It knows
// nothing about how the printed document is laid out — that stays the
// consuming app's concern.
type SignatureHandler struct {
	assignments   AssignmentReader
	signer        *service.SignatureService
	publicBaseURL string
}

func NewSignatureHandler(assignments AssignmentReader, signer *service.SignatureService, publicBaseURL string) *SignatureHandler {
	return &SignatureHandler{assignments: assignments, signer: signer, publicBaseURL: publicBaseURL}
}

// QR returns a PNG for one assignment's approval stamp.
//
// @Summary Approval stamp QR code
// @Description PNG QR code encoding a signed verify link for this assignment, meant to be printed on a paper document as a "signature" stamp. Not a certified/legally-binding e-signature — a tamper-evident proof that an approval genuinely happened in this engine.
// @Tags signature
// @Produce png
// @Param id path string true "Assignment ID"
// @Success 200 {file} binary "image/png"
// @Failure 404 {object} response.Envelope "assignment not found"
// @Router /assignments/{id}/qr [get]
func (h *SignatureHandler) QR(c *fiber.Ctx) error {
	id := c.Params("id")
	assignment, err := h.assignments.GetAssignmentByID(c.Context(), id)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	if assignment == nil {
		return response.Error(c, fiber.StatusNotFound, "assignment not found")
	}

	sig := h.signer.Sign(assignment.ID)
	verifyURL := fmt.Sprintf("%s/api/v1/verify/%s?sig=%s", h.publicBaseURL, assignment.ID, sig)

	png, err := qrcode.Encode(verifyURL, qrcode.Medium, 256)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "failed to generate qr code")
	}

	c.Set(fiber.HeaderContentType, "image/png")
	return c.Send(png)
}

// Verify is the public landing page for a scanned stamp.
//
// @Summary Verify an approval stamp
// @Description Public: confirms the HMAC code is genuine, then reports the assignment's real, live status (never trusts the code alone for the actual decision).
// @Tags signature
// @Produce json
// @Param id path string true "Assignment ID"
// @Param sig query string true "HMAC signature from the QR code"
// @Success 200 {object} response.Envelope
// @Failure 403 {object} response.Envelope "invalid or tampered signature"
// @Failure 404 {object} response.Envelope "assignment not found"
// @Router /verify/{id} [get]
func (h *SignatureHandler) Verify(c *fiber.Ctx) error {
	id := c.Params("id")
	sig := c.Query("sig")

	if sig == "" || !h.signer.Verify(id, sig) {
		return response.Error(c, fiber.StatusForbidden, "invalid or tampered verification code")
	}

	assignment, err := h.assignments.GetAssignmentByID(c.Context(), id)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	if assignment == nil {
		return response.Error(c, fiber.StatusNotFound, "assignment not found")
	}

	return response.Success(c, fiber.StatusOK, "verified", fiber.Map{
		"assignment_id": assignment.ID,
		"user_id":       assignment.UserID,
		"user_name":     assignment.UserName,
		"user_position": assignment.UserPosition,
		"status":        assignment.Status,
		"acted_at":      assignment.ActedAt,
		"comment":       assignment.Comment,
	})
}
