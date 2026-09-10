package handler

import (
	"context"

	"github.com/gofiber/fiber/v2"

	"approval-engine-service/internal/domain"
	"approval-engine-service/internal/middleware"
	"approval-engine-service/internal/service"
	"approval-engine-service/pkg/response"
)

// RequestReader is the read-side the handler needs; satisfied by
// *repository.RequestRepository.
type RequestReader interface {
	GetByID(ctx context.Context, id string) (*domain.ApprovalRequest, error)
	FindByResource(ctx context.Context, appID, docType, resourceID string) (*domain.ApprovalRequest, error)
}

type RequestHandler struct {
	engine   *service.Engine
	requests RequestReader
}

func NewRequestHandler(engine *service.Engine, requests RequestReader) *RequestHandler {
	return &RequestHandler{engine: engine, requests: requests}
}

// CreateRequestBody is the payload for POST /requests.
type CreateRequestBody struct {
	DocType     string         `json:"doc_type" example:"purchase_order"`
	ResourceID  string         `json:"resource_id" example:"PO-2026-0912"`
	RequesterID string         `json:"requester_id" example:"SA01"`
	Payload     map[string]any `json:"payload"`
}

// Create starts a new approval request.
//
// @Summary Create an approval request
// @Description Starts a new request against the calling application's currently active workflow for doc_type. Idempotent on (app, doc_type, resource_id): calling it again with the same values returns the existing request instead of erroring, so a Spring Boot caller can safely retry on timeout. requester_id must be a registered, active participant.
// @Tags requests
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param body body CreateRequestBody true "Request to create"
// @Success 201 {object} response.Envelope{data=domain.ApprovalRequest} "created"
// @Success 200 {object} response.Envelope{data=domain.ApprovalRequest} "already existed (idempotent replay)"
// @Failure 400 {object} response.Envelope "validation error or unregistered requester"
// @Failure 401 {object} response.Envelope "missing or invalid X-API-Key"
// @Router /requests [post]
func (h *RequestHandler) Create(c *fiber.Ctx) error {
	appID, _ := c.Locals(middleware.LocalsAppID).(string)
	if appID == "" {
		return response.Error(c, fiber.StatusUnauthorized, "missing application context")
	}

	var body CreateRequestBody
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if body.DocType == "" || body.ResourceID == "" || body.RequesterID == "" {
		return response.Error(c, fiber.StatusBadRequest, "doc_type, resource_id and requester_id are required")
	}

	existing, err := h.requests.FindByResource(c.Context(), appID, body.DocType, body.ResourceID)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	if existing != nil {
		return response.Success(c, fiber.StatusOK, "request already exists for this resource", existing)
	}

	req, err := h.engine.CreateRequest(c.Context(), appID, body.DocType, body.ResourceID, body.RequesterID, body.Payload)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, fiber.StatusCreated, "request created", req)
}

// Get returns one request with its steps and assignments.
//
// @Summary Get a request
// @Description Full request with materialized steps and assignment snapshots. Not gated behind an API key: both the consuming app and the approver portal need to read this, and the id itself is an unguessable UUID.
// @Tags requests
// @Produce json
// @Param id path string true "Request ID"
// @Success 200 {object} response.Envelope{data=domain.ApprovalRequest}
// @Failure 404 {object} response.Envelope "not found"
// @Router /requests/{id} [get]
func (h *RequestHandler) Get(c *fiber.Ctx) error {
	req, err := h.requests.GetByID(c.Context(), c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	if req == nil {
		return response.Error(c, fiber.StatusNotFound, "request not found")
	}
	return response.Success(c, fiber.StatusOK, "", req)
}

// DecisionBody is the payload for POST /requests/{id}/decision.
type DecisionBody struct {
	UserID   string  `json:"user_id" example:"SS01"`
	Decision string  `json:"decision" example:"approved" enums:"approved,rejected"`
	Comment  *string `json:"comment,omitempty"`
}

// Decide records one approver's decision.
//
// @Summary Approve or reject the active step
// @Description The engine enforces that user_id is the person assigned to the currently active step (and still an active participant) — this handler does not independently verify the caller's identity beyond that.
// @Tags requests
// @Accept json
// @Produce json
// @Param id path string true "Request ID"
// @Param body body DecisionBody true "Decision"
// @Success 200 {object} response.Envelope{data=domain.ApprovalRequest}
// @Failure 400 {object} response.Envelope "not the assigned approver, already decided, or invalid decision"
// @Router /requests/{id}/decision [post]
func (h *RequestHandler) Decide(c *fiber.Ctx) error {
	var body DecisionBody
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if body.UserID == "" {
		return response.Error(c, fiber.StatusBadRequest, "user_id is required")
	}
	if body.Decision != domain.StatusApproved && body.Decision != domain.StatusRejected {
		return response.Error(c, fiber.StatusBadRequest, "decision must be 'approved' or 'rejected'")
	}

	req, err := h.engine.RecordDecision(c.Context(), c.Params("id"), body.UserID, body.Decision, body.Comment)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.Success(c, fiber.StatusOK, "decision recorded", req)
}
