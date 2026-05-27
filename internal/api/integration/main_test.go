package integration_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/api/testhelpers"
)

var testDB *testhelpers.TestDB

func TestMain(m *testing.M) {
	// If DOCKER_HOST is not set and a Colima socket exists, set it. This
	// helps macOS devs using Colima but avoids overriding a working Docker
	// environment on CI runners.
	if os.Getenv("DOCKER_HOST") == "" {
		colimaSock := fmt.Sprintf("%s/.colima/default/docker.sock", os.Getenv("HOME"))
		if _, err := os.Stat(colimaSock); err == nil {
			_ = os.Setenv("DOCKER_HOST", "unix://"+colimaSock)
		}
	}
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")

	tdb, err := testhelpers.SetupPostgres(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "SKIPPING integration tests (Docker unavailable):", err)
		os.Exit(0)
	}
	testDB = tdb

	code := m.Run()

	testDB.TearDown(nil)
	os.Exit(code)
}
