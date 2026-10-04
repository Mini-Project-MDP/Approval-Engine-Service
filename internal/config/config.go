package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all environment-driven configuration for the service.
type Config struct {
	AppEnv  string
	AppPort string

	DatabaseURL       string
	DatabaseAuthToken string
	DatabaseReset     bool

	AllowedOrigins string

	// PublicBaseURL is embedded into the QR/verify link printed on documents,
	// so it must be reachable by whoever scans it (not localhost, outside dev).
	PublicBaseURL string

	// VerificationSecret signs the QR/barcode "signature" stamp printed on
	// approved documents. Never expose it; anyone with it could forge a
	// verifiable-looking approval stamp.
	VerificationSecret string

	// AdminAPIKey gates operator-only endpoints that change data every
	// consuming app depends on (e.g. importing the org chart). It is an ops
	// credential for CLI tools/scripts, never shipped to a browser. Empty
	// means those endpoints are disabled.
	AdminAPIKey string

	// PortalEnabled toggles the /portal routes the browser portal uses. They
	// have no real user authentication until SSO login is wired in, so a
	// deployment that must be locked down can switch them off.
	PortalEnabled bool
}

// Load reads a .env file (if present) and builds a Config from environment
// variables, falling back to sane defaults for local development.
func Load() *Config {
	_ = godotenv.Load()

	// Vercel (and most PaaS runtimes) assign the listen port via PORT and
	// expect the server to bind to it; APP_PORT remains the local-dev override.
	appPort := getEnv("PORT", getEnv("APP_PORT", "8000"))

	return &Config{
		AppEnv:  getEnv("APP_ENV", "development"),
		AppPort: appPort,

		DatabaseURL:       getEnv("DATABASE_URL", ""),
		DatabaseAuthToken: getEnv("DATABASE_AUTH_TOKEN", ""),
		DatabaseReset:     getEnv("DB_RESET", "false") == "true",

		AllowedOrigins: getEnv("ALLOWED_ORIGINS", "http://localhost:5173"),

		PublicBaseURL:      getEnv("PUBLIC_BASE_URL", fmt.Sprintf("http://localhost:%s", appPort)),
		VerificationSecret: getEnv("VERIFICATION_SECRET", ""),

		AdminAPIKey:   getEnv("ADMIN_API_KEY", ""),
		PortalEnabled: getEnv("PORTAL_ENABLED", "true") == "true",
	}
}

// HasDatabase reports whether enough info was provided to attempt a DB connection.
func (c *Config) HasDatabase() bool {
	return c.DatabaseURL != ""
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
