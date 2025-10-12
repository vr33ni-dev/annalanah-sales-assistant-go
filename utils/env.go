package utils

import (
	"os"
	"strings"
)

// IsLocalEnv returns true if the app is running in a local development environment.
func IsLocalEnv() bool {
	env := strings.ToLower(os.Getenv("APP_ENV"))
	return env == "local" || env == "localhost"
}

// IsProdEnv returns true if the app is running in production.
func IsProdEnv() bool {
	env := strings.ToLower(os.Getenv("APP_ENV"))
	return env == "prod" || env == "production"
}
