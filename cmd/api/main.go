package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"

	_ "approval-engine-service/docs"
	"approval-engine-service/internal/config"
	"approval-engine-service/internal/database"
	"approval-engine-service/internal/handler"
	"approval-engine-service/internal/middleware"
	"approval-engine-service/internal/repository"
	"approval-engine-service/internal/router"
	"approval-engine-service/internal/service"
	"approval-engine-service/pkg/response"
)

// @title Approval Engine API
// @version 1.0
// @description Reusable approval workflow engine shared across Mayora internal systems (asset management first). A consuming app's requester_id/user_id are always checked against registered, active participants — the engine never trusts an identity blindly.
// @description Auth: every /requests route requires X-API-Key (a consuming application's server-to-server key — never expose it in a browser), and an application only ever sees and decides its own requests. /participants/import requires the operator X-Admin-Key. /portal/* serves the browser portal and can be switched off with PORTAL_ENABLED=false until SSO login is in place.
// @BasePath /api/v1
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-API-Key
// @securityDefinitions.apikey AdminKeyAuth
// @in header
// @name X-Admin-Key
func main() {
	cfg := config.Load()

	if !cfg.HasDatabase() {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := database.NewTurso(cfg)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()
	log.Println("connected to database")

	if cfg.DatabaseReset {
		if cfg.AppEnv != "development" {
			log.Fatal("DB_RESET is only allowed when APP_ENV=development")
		}
		if err := database.Reset(db); err != nil {
			log.Fatalf("reset failed: %v", err)
		}
		log.Println("all tables dropped (DB_RESET=true)")
	}

	if err := database.Migrate(db); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	log.Println("schema applied")

	if cfg.VerificationSecret == "" {
		if cfg.AppEnv != "development" {
			log.Fatal("VERIFICATION_SECRET is required outside development")
		}
		log.Println("WARNING: VERIFICATION_SECRET is empty, using an insecure dev default")
		cfg.VerificationSecret = "dev-only-insecure-secret"
	}

	participants := repository.NewParticipantRepository(db)
	workflows := repository.NewWorkflowRepository(db)
	requests := repository.NewRequestRepository(db)
	events := repository.NewEventRepository(db)
	applications := repository.NewApplicationRepository(db)

	resolver := service.NewResolver(participants)
	notifier := service.NewWebhookNotifier(applications)
	engine := service.NewEngine(workflows, requests, events, resolver, participants, notifier)
	signer := service.NewSignatureService(cfg.VerificationSecret)

	deps := router.Dependencies{
		Health:        handler.NewHealthHandler(),
		Requests:      handler.NewRequestHandler(engine, requests),
		Inbox:         handler.NewInboxHandler(requests),
		Signature:     handler.NewSignatureHandler(requests, signer, cfg.PublicBaseURL),
		Applications:  handler.NewApplicationHandler(applications),
		Workflows:     handler.NewWorkflowHandler(workflows),
		Participants:  handler.NewParticipantHandler(participants),
		APIKeyLookup:  applications,
		AdminAPIKey:   cfg.AdminAPIKey,
		PortalEnabled: cfg.PortalEnabled,
	}

	app := fiber.New(fiber.Config{
		AppName:      "Approval Engine Service",
		ErrorHandler: errorHandler,
	})

	middleware.Register(app, cfg)
	router.Register(app, deps)

	go func() {
		if err := app.Listen(":" + cfg.AppPort); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()
	log.Printf("server listening on :%s (env: %s)", cfg.AppPort, cfg.AppEnv)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down gracefully...")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	log.Println("server stopped")
}

func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	return response.Error(c, code, err.Error())
}
