package api

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv             string // local | dev | prod
	Port               string // e.g. 8080; Render provides PORT
	DatabaseURL        string
	GoogleClientID     string
	GoogleClientSecret string
	AllowedEmails      []string // comma-separated
	CookieSigningKey   string
	OAuthRedirectURL   string
	PostLoginRedirect  string
	CORSOrigins        []string // comma-separated
}

func LoadConfig() (*Config, error) {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if env == "" {
		env = "local"
	}

	// Only read .env files when running locally.
	if env == "local" {
		_ = godotenv.Load(".env")           // optional, shared defaults
		_ = godotenv.Overload(".env.local") // laptop-only secrets
	}

	cfg := &Config{
		AppEnv:             env,
		Port:               fallback(os.Getenv("PORT"), "8080"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		AllowedEmails:      splitCSV(os.Getenv("ALLOWED_EMAILS")),
		CookieSigningKey:   os.Getenv("COOKIE_SIGNING_KEY"),
		OAuthRedirectURL:   os.Getenv("OAUTH_REDIRECT_URL"),
		PostLoginRedirect:  os.Getenv("POST_LOGIN_REDIRECT"),
		CORSOrigins:        splitCSV(os.Getenv("CORS_ORIGINS")),
	}
	return cfg, nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fallback(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
