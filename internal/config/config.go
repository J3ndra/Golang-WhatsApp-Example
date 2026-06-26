package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all configuration for the WhatsApp webhook server.
type Config struct {
	Port            string
	VerifyToken     string
	PhoneNumberID   string
	AccessToken     string
	GraphAPIBaseURL string
	APIVersion      string
	DBPath          string
}

// Load reads configuration from environment variables, applies defaults,
// and validates that all required fields are present.
func Load() (*Config, error) {
	cfg := &Config{
		Port:            envOrDefault("PORT", "8080"),
		VerifyToken:     os.Getenv("WHATSAPP_VERIFY_TOKEN"),
		PhoneNumberID:   os.Getenv("WHATSAPP_PHONE_NUMBER_ID"),
		AccessToken:     os.Getenv("WHATSAPP_ACCESS_TOKEN"),
		GraphAPIBaseURL: envOrDefault("GRAPH_API_BASE_URL", "https://graph.facebook.com"),
		APIVersion:      envOrDefault("WHATSAPP_API_VERSION", "v22.0"),
		DBPath:          envOrDefault("DB_PATH", "kiw.db"),
	}

	var missing []string
	if cfg.VerifyToken == "" {
		missing = append(missing, "WHATSAPP_VERIFY_TOKEN")
	}
	if cfg.PhoneNumberID == "" {
		missing = append(missing, "WHATSAPP_PHONE_NUMBER_ID")
	}
	if cfg.AccessToken == "" {
		missing = append(missing, "WHATSAPP_ACCESS_TOKEN")
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"missing required environment variables: [%s]",
			strings.Join(missing, " "),
		)
	}

	return cfg, nil
}

func envOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
