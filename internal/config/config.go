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
}

// Load reads a .env file (if present) and builds a Config from environment
// variables, falling back to sane defaults for local development.
func Load() *Config {
	_ = godotenv.Load()

	appPort := getEnv("APP_PORT", "8000")

	return &Config{
		AppEnv:  getEnv("APP_ENV", "development"),
		AppPort: appPort,

		DatabaseURL:       getEnv("DATABASE_URL", ""),
		DatabaseAuthToken: getEnv("DATABASE_AUTH_TOKEN", ""),
		DatabaseReset:     getEnv("DB_RESET", "false") == "true",

		AllowedOrigins: getEnv("ALLOWED_ORIGINS", "http://localhost:5173"),

		PublicBaseURL:      getEnv("PUBLIC_BASE_URL", fmt.Sprintf("http://localhost:%s", appPort)),
		VerificationSecret: getEnv("VERIFICATION_SECRET", ""),
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
