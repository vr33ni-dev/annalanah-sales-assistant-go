// file: api/config_test.go
package api

import (
	"reflect"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b", []string{"a", "b"}},
		{" a,  b , , c ", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		got := splitCSV(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("splitCSV(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestFallback(t *testing.T) {
	if got := fallback("", "def"); got != "def" {
		t.Fatalf("fallback empty = %q, want %q", got, "def")
	}
	if got := fallback("x", "def"); got != "x" {
		t.Fatalf("fallback non-empty = %q, want %q", got, "x")
	}
}

func TestLoadConfig_LocalDefaults(t *testing.T) {
	// Make sure we start from a clean slate for relevant vars
	clearEnv := []string{
		"APP_ENV",
		"PORT",
		"DATABASE_URL",
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"ALLOWED_EMAILS",
		"COOKIE_SIGNING_KEY",
		"OAUTH_REDIRECT_URL",
		"POST_LOGIN_REDIRECT",
		"CORS_ORIGINS",
	}
	for _, k := range clearEnv {
		t.Setenv(k, "")
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.AppEnv != "local" {
		t.Fatalf("AppEnv = %q, want %q", cfg.AppEnv, "local")
	}
	if cfg.Port != "8080" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.AllowedEmails != nil {
		t.Fatalf("AllowedEmails = %#v, want nil for empty env", cfg.AllowedEmails)
	}
	if cfg.CORSOrigins != nil {
		t.Fatalf("CORSOrigins = %#v, want nil for empty env", cfg.CORSOrigins)
	}
}

func TestLoadConfig_WithEnvValues(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("PORT", "9000")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("GOOGLE_CLIENT_ID", "gid")
	t.Setenv("GOOGLE_CLIENT_SECRET", "gsecret")
	t.Setenv("ALLOWED_EMAILS", "a@example.com,b@example.com")
	t.Setenv("COOKIE_SIGNING_KEY", "super-secret-key")
	t.Setenv("OAUTH_REDIRECT_URL", "https://app.example.com/auth/callback")
	t.Setenv("POST_LOGIN_REDIRECT", "https://app.example.com/")
	t.Setenv("CORS_ORIGINS", "https://app.example.com,https://admin.example.com")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.AppEnv != "prod" {
		t.Fatalf("AppEnv = %q, want %q", cfg.AppEnv, "prod")
	}
	if cfg.Port != "9000" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "9000")
	}
	if cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if !reflect.DeepEqual(cfg.AllowedEmails, []string{"a@example.com", "b@example.com"}) {
		t.Fatalf("AllowedEmails = %#v", cfg.AllowedEmails)
	}
	if !reflect.DeepEqual(cfg.CORSOrigins, []string{
		"https://app.example.com",
		"https://admin.example.com",
	}) {
		t.Fatalf("CORSOrigins = %#v", cfg.CORSOrigins)
	}
	if cfg.OAuthRedirectURL != "https://app.example.com/auth/callback" {
		t.Fatalf("OAuthRedirectURL = %q", cfg.OAuthRedirectURL)
	}
	if cfg.PostLoginRedirect != "https://app.example.com/" {
		t.Fatalf("PostLoginRedirect = %q", cfg.PostLoginRedirect)
	}
	if cfg.CookieSigningKey != "super-secret-key" {
		t.Fatalf("CookieSigningKey = %q", cfg.CookieSigningKey)
	}
}
