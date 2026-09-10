// smoketest seeds a minimal application + participant chain + workflow
// against the real Turso database and runs one request through the engine
// end to end. It exercises the same repository code the HTTP API will use,
// so it catches driver/SQL issues the fake-backed unit tests cannot see.
//
// Safe to re-run: it resets the schema first (DB_RESET semantics), so this
// also doubles as a quick way to get a clean demo dataset.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"approval-engine-service/internal/config"
	"approval-engine-service/internal/database"
	"approval-engine-service/internal/domain"
	"approval-engine-service/internal/repository"
	"approval-engine-service/internal/service"
)

func main() {
	ctx := context.Background()
	cfg := config.Load()

	db, err := database.NewTurso(cfg)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer db.Close()

	if err := database.Reset(db); err != nil {
		log.Fatalf("reset: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	fmt.Println("schema reset and applied")

	participants := repository.NewParticipantRepository(db)
	workflows := repository.NewWorkflowRepository(db)
	requests := repository.NewRequestRepository(db)
	events := repository.NewEventRepository(db)
	resolver := service.NewResolver(participants)
	engine := service.NewEngine(workflows, requests, events, resolver, participants)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO applications (id, code, name, api_key) VALUES (?, ?, ?, ?)`,
		"assetmgmt", "assetmgmt", "Asset Management", "demo-key-assetmgmt"); err != nil {
		log.Fatalf("seed application: %v", err)
	}
	fmt.Println("seeded application")

	// Sales chain: Sales Admin -> Supervisor -> RSM, then a cross-function
	// hop to a System Support asset officer.
	ptr := func(s string) *string { return &s }
	// Deliberately NOT top-down (SA01 references SS01 before SS01 appears):
	// a real FICOM export won't be topologically sorted either, and
	// UpsertBatch defers FK checks to commit time precisely for this.
	seed := []domain.Participant{
		{UserID: "SA01", Name: "Sales Admin", Position: "Sales Admin", Department: "Sales", SuperiorID: ptr("SS01"), IsActive: true},
		{UserID: "SS01", Name: "Siti (Supervisor)", Position: "SS", Department: "Sales", SuperiorID: ptr("RSM1"), IsActive: true},
		{UserID: "RSM1", Name: "Rudi (RSM)", Position: "RSM", Department: "Sales", IsActive: true},
		{UserID: "AST01", Name: "Andi (Asset Officer)", Position: "Asset Officer", Department: "System Support", IsActive: true},
	}
	if err := participants.UpsertBatch(ctx, seed); err != nil {
		log.Fatalf("seed participants: %v", err)
	}
	fmt.Printf("seeded %d participants\n", len(seed))

	def := &domain.WorkflowDefinition{
		ID: "def-demo-po", AppID: "assetmgmt", DocType: "purchase_order", Name: "PO Approval", Version: 1, IsActive: true,
		Steps: []domain.WorkflowStep{
			{
				ID: "step-1", StepOrder: 1, Name: "Supervisor Approval",
				ResolverRule: domain.ResolverRule{Type: domain.ResolverSuperior, Level: 1},
				ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail,
			},
			{
				ID: "step-2", StepOrder: 2, Name: "Asset Team Approval",
				ResolverRule: domain.ResolverRule{Type: domain.ResolverRole, Position: "Asset Officer", Department: "System Support"},
				ApprovalMode: domain.ModeAny, OnEmpty: domain.OnEmptyFail,
			},
		},
	}
	if err := workflows.CreateDefinition(ctx, def); err != nil {
		log.Fatalf("seed workflow: %v", err)
	}
	fmt.Println("seeded workflow definition with 2 steps")

	req, err := engine.CreateRequest(ctx, "assetmgmt", "purchase_order", "PO-DEMO-1", "SA01", map[string]any{"amount": float64(2000000)})
	if err != nil {
		log.Fatalf("create request: %v", err)
	}
	printRequest("after create", req)

	req, err = engine.RecordDecision(ctx, req.ID, "SS01", domain.StatusApproved, ptr("looks fine"))
	if err != nil {
		log.Fatalf("approve step 1: %v", err)
	}
	printRequest("after supervisor approves", req)

	req, err = engine.RecordDecision(ctx, req.ID, "AST01", domain.StatusApproved, nil)
	if err != nil {
		log.Fatalf("approve step 2: %v", err)
	}
	printRequest("after asset team approves (should be approved & complete)", req)

	audit, err := events.ListByRequest(ctx, req.ID)
	if err != nil {
		log.Fatalf("list events: %v", err)
	}
	fmt.Println("\naudit trail:")
	for _, e := range audit {
		fmt.Printf("  [%s] %s\n", e.CreatedAt, e.EventType)
	}
}

func printRequest(label string, req *domain.ApprovalRequest) {
	fmt.Printf("\n--- %s ---\n", label)
	fmt.Printf("status=%s current_step=%d\n", req.Status, req.CurrentStepOrder)
	for _, s := range req.Steps {
		fmt.Printf("  step %d [%s] status=%s\n", s.StepOrder, s.Name, s.Status)
		for _, a := range s.Assignments {
			fmt.Printf("    - %s (%s): %s\n", a.UserID, a.UserPosition, a.Status)
		}
	}
	b, _ := json.Marshal(req.Payload)
	fmt.Printf("payload=%s\n", b)
}
