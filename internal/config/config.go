package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Environment represents the execution environment FSM state.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvProduction  Environment = "production"
)

// MaskedEmail is a branded type for PII-safe logging.
type MaskedEmail string

// MaskEmail converts a raw email address into a PII-safe MaskedEmail for structured logs.
func MaskEmail(raw string) MaskedEmail {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	parts := strings.Split(trimmed, "@")
	if len(parts) != 2 || len(parts[0]) == 0 {
		return MaskedEmail("***@invalid")
	}
	local := parts[0]
	domain := parts[1]
	if len(local) <= 2 {
		return MaskedEmail(local[:1] + "***@" + domain)
	}
	return MaskedEmail(local[:2] + "***@" + domain)
}

// CanonicalSuperAdmins defines the immutable set of platform super administrators.
var CanonicalSuperAdmins = map[string]struct{}{
	"oana.becheanu@yahoo.com":     {},
	"george.becheanu11@yahoo.com": {},
}

// IsSuperAdminEmail checks deterministically whether an email belongs to an isolated Super Admin.
func IsSuperAdminEmail(email string) bool {
	normalized := strings.TrimSpace(strings.ToLower(email))
	_, ok := CanonicalSuperAdmins[normalized]
	return ok
}

// Config holds validated application configuration.
type Config struct {
	Env              Environment
	Port             int
	DatabasePath     string
	HMACSecret       string
	AppURL           string
	R2AccountID      string
	R2AccessKeyID    string
	R2SecretKey      string
	R2BucketName     string
	R2PublicURL      string
	SMTPHost         string
	SMTPPort         int
	SMTPSecure       bool
	SMTPUser         string
	SMTPPass         string
	EmailFrom        string
	SuperAdminEmails []string
}

// Load parses and validates environment variables with strict fail-fast invariants.
func Load() (*Config, error) {
	rawEnv := strings.TrimSpace(strings.ToLower(getEnv("APP_ENV", getEnv("NODE_ENV", "development"))))
	var env Environment
	switch rawEnv {
	case "production":
		env = EnvProduction
	case "test":
		env = EnvTest
	default:
		env = EnvDevelopment
	}

	portStr := getEnv("PORT", "8080")
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("config: invalid PORT %q: must be between 1 and 65535", portStr)
	}

	dbPath := strings.TrimPrefix(getEnv("DATABASE_URL", "local.db"), "file:")
	if dbPath == "" {
		return nil, errors.New("config: DATABASE_URL cannot be empty")
	}

	hmacSecret := getEnv("HMAC_SECRET", "default-dev-hmac-secret-min-16-chars")
	if len(hmacSecret) < 16 {
		return nil, errors.New("config: HMAC_SECRET must be at least 16 characters long")
	}
	if env == EnvProduction && hmacSecret == "default-dev-hmac-secret-min-16-chars" {
		return nil, errors.New("config: production environment requires a non-default HMAC_SECRET")
	}

	smtpPort, err := strconv.Atoi(getEnv("SMTP_PORT", "465"))
	if err != nil || smtpPort <= 0 {
		smtpPort = 465
	}

	return &Config{
		Env:          env,
		Port:         port,
		DatabasePath: dbPath,
		HMACSecret:   hmacSecret,
		AppURL:       getEnv("NEXT_PUBLIC_APP_URL", "http://localhost:8080"),
		R2AccountID:  getEnv("R2_ACCOUNT_ID", ""),
		R2AccessKeyID: getEnv("R2_ACCESS_KEY_ID", ""),
		R2SecretKey:  getEnv("R2_SECRET_ACCESS_KEY", ""),
		R2BucketName: getEnv("R2_BUCKET_NAME", "instalbloc-media"),
		R2PublicURL:  getEnv("R2_PUBLIC_URL", "https://media.instalbloc.ro"),
		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPPort:     smtpPort,
		SMTPSecure:   getEnv("SMTP_SECURE", "true") == "true",
		SMTPUser:     getEnv("SMTP_USER", ""),
		SMTPPass:     getEnv("SMTP_PASS", ""),
		EmailFrom:    getEnv("EMAIL_FROM", "contact@instalbloc.ro"),
		SuperAdminEmails: []string{
			"oana.becheanu@yahoo.com",
			"george.becheanu11@yahoo.com",
		},
	}, nil
}

func getEnv(key, fallback string) string {
	if val, exists := os.LookupEnv(key); exists && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return fallback
}
