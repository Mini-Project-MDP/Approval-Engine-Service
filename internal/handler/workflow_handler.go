package handler

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"approval-engine-service/internal/domain"
	"approval-engine-service/internal/service"
	"approval-engine-service/pkg/response"
)

type WorkflowStore interface {
	Publish(ctx context.Context, def *domain.WorkflowDefinition) error
	ListDefinitions(ctx context.Context, appID string) ([]domain.WorkflowDefinition, error)
	GetDefinitionByID(ctx context.Context, id string) (*domain.WorkflowDefinition, error)
	Deactivate(ctx context.Context, id string) error
}

// WorkflowHandler is the CRUD surface behind "add/edit/delete an approval
// flow without a code change". Editing publishes a new version rather than
// mutating one in place, so a request already in flight keeps running
// against the exact rules it started with (see WorkflowRepository.Publish).
type WorkflowHandler struct {
	workflows WorkflowStore
}

func NewWorkflowHandler(workflows WorkflowStore) *WorkflowHandler {
	return &WorkflowHandler{workflows: workflows}
}

// StepInput is one step in a PublishWorkflowBody.
type StepInput struct {
	Name         string              `json:"name" example:"Supervisor Approval"`
	ResolverRule domain.ResolverRule `json:"resolver_rule"`
	Condition    *domain.Condition   `json:"condition,omitempty"`
	ApprovalMode string              `json:"approval_mode" example:"any" enums:"any,all"`
	OnEmpty      string              `json:"on_empty" example:"fail" enums:"fail,skip"`
}

// PublishWorkflowBody is the payload for POST /workflows.
type PublishWorkflowBody struct {
	AppID   string      `json:"app_id" example:"assetmgmt"`
	DocType string      `json:"doc_type" example:"purchase_order"`
	Name    string      `json:"name" example:"PO Approval"`
	Steps   []StepInput `json:"steps"`
}

// Create publishes a new version of a workflow.
//
// @Summary Publish a workflow (create or edit)
// @Description Always inserts a brand new version and atomically deactivates every other version of (app_id, doc_type) — same call whether the pair is new or already exists. In-flight requests keep running against the exact version they started with. Each step's resolver_rule/condition is validated immediately (unknown type, missing field, bad operator all fail here, not silently at resolve time).
// @Tags workflows
// @Accept json
// @Produce json
// @Param body body PublishWorkflowBody true "Workflow to publish"
// @Success 201 {object} response.Envelope{data=domain.WorkflowDefinition}
// @Failure 400 {object} response.Envelope "validation error"
// @Router /workflows [post]
func (h *WorkflowHandler) Create(c *fiber.Ctx) error {
	var body PublishWorkflowBody
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if body.AppID == "" || body.DocType == "" || body.Name == "" {
		return response.Error(c, fiber.StatusBadRequest, "app_id, doc_type and name are required")
	}
	if len(body.Steps) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "at least one step is required")
	}

	def := &domain.WorkflowDefinition{
		ID: uuid.NewString(), AppID: body.AppID, DocType: body.DocType, Name: body.Name,
	}
	for i, s := range body.Steps {
		mode := s.ApprovalMode
		if mode == "" {
			mode = domain.ModeAny
		}
		onEmpty := s.OnEmpty
		if onEmpty == "" {
			onEmpty = domain.OnEmptyFail
		}
		step := domain.WorkflowStep{
			ID: uuid.NewString(), StepOrder: i + 1, Name: s.Name,
			ResolverRule: s.ResolverRule, Condition: s.Condition, ApprovalMode: mode, OnEmpty: onEmpty,
		}
		if err := service.ValidateStep(step); err != nil {
			return response.Error(c, fiber.StatusBadRequest, fmt.Sprintf("step %d: %s", i+1, err.Error()))
		}
		def.Steps = append(def.Steps, step)
	}

	if err := h.workflows.Publish(c.Context(), def); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	for i := range def.Steps {
		def.Steps[i].DefinitionID = def.ID
	}
	return response.Success(c, fiber.StatusCreated, "workflow published", def)
}

// List returns every version of every workflow.
//
// @Summary List workflows
// @Description Every version of every workflow (optionally filtered by app_id), for the management list view.
// @Tags workflows
// @Produce json
// @Param app_id query string false "Filter by application ID"
// @Success 200 {object} response.Envelope{data=[]domain.WorkflowDefinition}
// @Router /workflows [get]
func (h *WorkflowHandler) List(c *fiber.Ctx) error {
	defs, err := h.workflows.ListDefinitions(c.Context(), c.Query("app_id"))
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, fiber.StatusOK, "", defs)
}

// Get returns one workflow version with its steps.
//
// @Summary Get a workflow version
// @Tags workflows
// @Produce json
// @Param id path string true "Workflow definition ID"
// @Success 200 {object} response.Envelope{data=domain.WorkflowDefinition}
// @Failure 404 {object} response.Envelope "not found"
// @Router /workflows/{id} [get]
func (h *WorkflowHandler) Get(c *fiber.Ctx) error {
	def, err := h.workflows.GetDefinitionByID(c.Context(), c.Params("id"))
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	if def == nil {
		return response.Error(c, fiber.StatusNotFound, "workflow not found")
	}
	return response.Success(c, fiber.StatusOK, "", def)
}

// Deactivate is the "delete".
//
// @Summary Deactivate a workflow version
// @Description Stops the version from being used for new requests. Never a hard delete — past requests still reference this definition_id and must stay readable.
// @Tags workflows
// @Produce json
// @Param id path string true "Workflow definition ID"
// @Success 200 {object} response.Envelope
// @Router /workflows/{id}/deactivate [post]
func (h *WorkflowHandler) Deactivate(c *fiber.Ctx) error {
	if err := h.workflows.Deactivate(c.Context(), c.Params("id")); err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.Success(c, fiber.StatusOK, "workflow deactivated", nil)
}
