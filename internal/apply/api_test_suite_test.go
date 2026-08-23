package apply

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGeneratesAPITestSuiteForAPISelection(t *testing.T) {
	tests := []struct {
		name            string
		raw             []byte
		wantIntegration bool
		wantAPI         bool
	}{
		{name: "api-only", raw: apiBlueprintBytes(), wantAPI: true},
		{name: "api-database", raw: fullMobileBlueprintBytes(), wantAPI: true, wantIntegration: true},
		{name: "without-api", raw: blueprintBytes()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destination := applyAPIWorkspace(t, tt.raw)
			apiRoot := filepath.Join(destination, "apis")
			if !tt.wantAPI {
				if _, err := os.Stat(filepath.Join(apiRoot, "internal", "server", "server_test.go")); !os.IsNotExist(err) {
					t.Fatalf("generated API tests exist for no-API selection: %v", err)
				}
				return
			}

			for _, relative := range []string{
				filepath.Join("internal", "server", "server_test.go"),
				filepath.Join("internal", "server", "config_test.go"),
				filepath.Join("internal", "server", "server_fuzz_test.go"),
			} {
				if _, err := os.Stat(filepath.Join(apiRoot, relative)); err != nil {
					t.Fatalf("missing generated API test %s: %v", relative, err)
				}
			}
			integrationPath := filepath.Join(apiRoot, "internal", "server", "database_integration_test.go")
			if tt.wantIntegration {
				if _, err := os.Stat(integrationPath); err != nil {
					t.Fatalf("missing conditional database integration test: %v", err)
				}
			} else if _, err := os.Stat(integrationPath); !os.IsNotExist(err) {
				t.Fatalf("database integration test exists for API-only selection: %v", err)
			}

			serverTest := readGeneratedAPIFile(t, apiRoot, filepath.Join("internal", "server", "server_test.go"))
			for _, marker := range []string{
				"func TestHealthzHandler",
				"func TestReadinessHandler",
				"func TestRequestIDMiddleware",
				"func TestRunStopsOnContextCancellation",
				"httptest.NewRecorder",
			} {
				if !strings.Contains(serverTest, marker) {
					t.Fatalf("generated server tests missing %q:\n%s", marker, serverTest)
				}
			}
			configTest := readGeneratedAPIFile(t, apiRoot, filepath.Join("internal", "server", "config_test.go"))
			for _, marker := range []string{"func TestLoadConfigDefaults", "func TestLoadConfigOverrides", "func TestLoadConfigRejectsMalformedValues"} {
				if !strings.Contains(configTest, marker) {
					t.Fatalf("generated config tests missing %q:\n%s", marker, configTest)
				}
			}
			fuzzTest := readGeneratedAPIFile(t, apiRoot, filepath.Join("internal", "server", "server_fuzz_test.go"))
			for _, marker := range []string{"func FuzzValidRequestID", "f.Add(\"request-123\")"} {
				if !strings.Contains(fuzzTest, marker) {
					t.Fatalf("generated fuzz test missing %q:\n%s", marker, fuzzTest)
				}
			}
			for _, forbidden := range []string{"testify", "gomega", "goconvey", "/users", "CRUD", "Authorization:"} {
				if strings.Contains(strings.ToLower(serverTest+configTest+fuzzTest), strings.ToLower(forbidden)) {
					t.Fatalf("generated API test suite contains forbidden marker %q", forbidden)
				}
			}
		})
	}
}

func TestApplyGeneratesConditionalDatabaseIntegrationTest(t *testing.T) {
	destination := applyAPIWorkspace(t, fullMobileBlueprintBytes())
	integration := readGeneratedAPIFile(t, filepath.Join(destination, "apis"), filepath.Join("internal", "server", "database_integration_test.go"))
	for _, marker := range []string{
		"//go:build integration",
		"DATABASE_URL is required",
		"pgxpool.New",
		"pool.Ping",
	} {
		if !strings.Contains(integration, marker) {
			t.Fatalf("conditional integration test missing %q:\n%s", marker, integration)
		}
	}
}

func TestApplyAPITestSuiteOutputIsByteStable(t *testing.T) {
	var outputs [2]map[string][]byte
	for i := range outputs {
		destination := applyAPIWorkspace(t, apiBlueprintBytes())
		apiRoot := filepath.Join(destination, "apis")
		outputs[i] = make(map[string][]byte)
		for _, relative := range []string{
			filepath.Join("internal", "server", "server_test.go"),
			filepath.Join("internal", "server", "config_test.go"),
			filepath.Join("internal", "server", "server_fuzz_test.go"),
			"Taskfile.yml",
		} {
			contents, err := os.ReadFile(filepath.Join(apiRoot, relative))
			if err != nil {
				t.Fatal(err)
			}
			outputs[i][relative] = contents
		}
	}
	for relative := range outputs[0] {
		if !bytes.Equal(outputs[0][relative], outputs[1][relative]) {
			t.Fatalf("generated API test artifact %s is not byte-stable", relative)
		}
	}
}

func TestGeneratedAPITestTasksExposeRaceFuzzAndConditionalIntegration(t *testing.T) {
	apiDestination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiTaskfile := readGeneratedAPIFile(t, filepath.Join(apiDestination, "apis"), "Taskfile.yml")
	for _, task := range []string{"test:race", "test:fuzz"} {
		if !strings.Contains(apiTaskfile, "  "+task+":") {
			t.Fatalf("API-only Taskfile missing %s:\n%s", task, apiTaskfile)
		}
	}
	if strings.Contains(apiTaskfile, "test:integration:") || strings.Contains(apiTaskfile, "-tags=integration") {
		t.Fatalf("API-only Taskfile contains a database integration task:\n%s", apiTaskfile)
	}

	databaseDestination := applyAPIWorkspace(t, fullMobileBlueprintBytes())
	databaseTaskfile := readGeneratedAPIFile(t, filepath.Join(databaseDestination, "apis"), "Taskfile.yml")
	if !strings.Contains(databaseTaskfile, "test:integration:") || !strings.Contains(databaseTaskfile, "go test -tags=integration ./...") {
		t.Fatalf("API+Database Taskfile missing explicit integration task:\n%s", databaseTaskfile)
	}
}

func TestGeneratedAPITestSuiteRunsWithoutDatabase(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	cmd := exec.Command("go", "test", "./internal/server", "-count=1")
	cmd.Dir = apiRoot
	cmd.Env = append(os.Environ(), "GOCACHE=/private/tmp/smt-gocache", "GOPROXY=off", "GOSUMDB=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated API test suite did not run without a database: %v\n%s", err, output)
	}
}
