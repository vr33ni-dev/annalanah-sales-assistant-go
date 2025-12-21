package integration_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/api/testhelpers"
)

var testDB *testhelpers.TestDB

func TestMain(m *testing.M) {
	// Colima safety (harmless on CI)
	_ = os.Setenv(
		"DOCKER_HOST",
		fmt.Sprintf("unix://%s/.colima/default/docker.sock", os.Getenv("HOME")),
	)
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")

	testDB = testhelpers.SetupPostgres(nil)

	code := m.Run()

	testDB.TearDown(nil)
	os.Exit(code)
}
