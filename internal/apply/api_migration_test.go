package apply

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGeneratesConditionalAPIMigrationAssets(t *testing.T) {
	tests := []struct {
		name        string
		raw         []byte
		wantMigrate bool
	}{
		{name: "api-database", raw: fullMobileBlueprintBytes(), wantMigrate: true},
		{name: "api-only", raw: apiBlueprintBytes()},
		{name: "database-only", raw: databaseBlueprintBytes()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destination := applyAPIWorkspace(t, tt.raw)
			apiRoot := filepath.Join(destination, "apis")
			migrationDir := filepath.Join(apiRoot, "migrations")
			validateScript := filepath.Join(apiRoot, "scripts", "validate-migrations.sh")

			if !tt.wantMigrate {
				if tt.name == "database-only" {
					if _, err := os.Lstat(apiRoot); !os.IsNotExist(err) {
						t.Fatalf("database-only output contains an API repository: %v", err)
					}
					return
				}
				taskfile := readGeneratedAPIFile(t, apiRoot, "Taskfile.yml")
				for _, path := range []string{migrationDir, validateScript} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Fatalf("non API+Database output contains migration asset %s: %v", path, err)
					}
				}
				if strings.Contains(taskfile, "migrate") {
					t.Fatalf("non API+Database Taskfile contains migration commands:\n%s", taskfile)
				}
				envExample := readGeneratedAPIFile(t, apiRoot, ".env.example")
				if strings.Contains(envExample, "DATABASE_URL") {
					t.Fatalf("API-only .env.example contains Database metadata:\n%s", envExample)
				}
				return
			}
			taskfile := readGeneratedAPIFile(t, apiRoot, "Taskfile.yml")

			for relative, want := range map[string]string{
				filepath.Join("migrations", "000001_baseline.up.sql"):   "SELECT 1;",
				filepath.Join("migrations", "000001_baseline.down.sql"): "SELECT 1;",
				filepath.Join("scripts", "validate-migrations.sh"):      "go run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate",
			} {
				contents := readGeneratedAPIFile(t, apiRoot, relative)
				if strings.HasSuffix(relative, ".sql") && contents != "SELECT 1;\n" {
					t.Fatalf("generated migration asset %s is not the deterministic no-op baseline: %q", relative, contents)
				}
				if !strings.Contains(contents, want) {
					t.Fatalf("generated migration asset %s missing %q:\n%s", relative, want, contents)
				}
			}

			envExample := readGeneratedAPIFile(t, apiRoot, ".env.example")
			if !strings.Contains(envExample, "DATABASE_URL=\n") {
				t.Fatalf("API+Database .env.example is missing an operator-provided DATABASE_URL:\n%s", envExample)
			}
			readme := readGeneratedAPIFile(t, apiRoot, "README.md")
			for _, want := range []string{
				"DATABASE_URL",
				"task migrate:create NAME=add_example",
				"task migrate:up",
				"task migrate:version",
				"task migrate:validate",
				"Apply does not provision a database or run migrations",
			} {
				if !strings.Contains(readme, want) {
					t.Fatalf("API+Database README missing migration guidance %q:\n%s", want, readme)
				}
			}

			wantTasks := []string{
				"build", "run", "test", "coverage", "test:race", "test:fuzz", "format:check",
				"lint", "vuln", "vet", "mod", "openapi", "container:build",
				"container:build:production", "container:verify", "test:integration",
				"migrate:create", "migrate:up", "migrate:version", "migrate:validate", "verify",
			}
			if got := taskfileTaskNames(taskfile); strings.Join(got, ",") != strings.Join(wantTasks, ",") {
				t.Fatalf("API+Database Taskfile tasks=%v, want %v:\n%s", got, wantTasks, taskfile)
			}
			for _, want := range []string{
				"MIGRATION_NAME: '{{.NAME}}'",
				"go run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate create -ext sql -dir migrations -seq \"$MIGRATION_NAME\"",
				"go run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database \"$DATABASE_URL\" up",
				"go run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database \"$DATABASE_URL\" version",
				"GOFLAGS: -tags=postgres",
				"sh scripts/validate-migrations.sh",
				"DATABASE_URL is required",
			} {
				if !strings.Contains(taskfile, want) {
					t.Fatalf("API+Database Taskfile missing %q:\n%s", want, taskfile)
				}
			}
			for _, forbidden := range []string{"migrate down", "migrate drop", "migrate force", "go install", "go get"} {
				if strings.Contains(taskfile+readGeneratedAPIFile(t, apiRoot, "scripts/validate-migrations.sh"), forbidden) {
					t.Fatalf("generated migration workflow contains forbidden command %q:\n%s", forbidden, taskfile)
				}
			}
			if verify := taskfileTaskBlock(taskfile, "verify"); strings.Contains(verify, "migrate") {
				t.Fatalf("verify unexpectedly runs migration tasks:\n%s", verify)
			}
		})
	}
}

func TestApplyGeneratesAPIMigrationAssetsByteIdentically(t *testing.T) {
	var outputs [2]map[string][]byte
	for i := range outputs {
		destination := applyAPIWorkspace(t, fullMobileBlueprintBytes())
		apiRoot := filepath.Join(destination, "apis")
		outputs[i] = make(map[string][]byte)
		for _, relative := range []string{
			filepath.Join("migrations", "000001_baseline.up.sql"),
			filepath.Join("migrations", "000001_baseline.down.sql"),
			filepath.Join("scripts", "validate-migrations.sh"),
			".env.example",
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
			t.Fatalf("generated migration asset %s is not byte-stable", relative)
		}
	}
}

func TestGeneratedAPIMigrationTasksUsePinnedPostgresCommandAndPropagateFailures(t *testing.T) {
	destination := applyAPIWorkspace(t, fullMobileBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "go.log")
	fakeGo := filepath.Join(fakeBin, "go")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$MIGRATE_LOG"
printf 'GOFLAGS=%s\n' "${GOFLAGS:-}" >> "$MIGRATE_LOG"
case "$*" in
  *" up")
    if [ "${MIGRATE_FAIL:-}" = "1" ]; then
      echo "migration is dirty or locked" >&2
      exit 17
    fi
    ;;
esac
exit 0
`
	if err := os.WriteFile(fakeGo, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(generatedEnvironmentWithoutConfig(),
		"PATH="+fakeBin+string(os.PathListSeparator)+"/usr/bin:/bin",
		"DATABASE_URL=postgres://smt@example.invalid/smt",
		"MIGRATE_LOG="+logPath,
	)
	taskPath := generatedE2ETaskBinary(t)
	for _, args := range [][]string{
		{"migrate:create", "NAME=add_widgets"},
		{"migrate:up"},
		{"migrate:version"},
		{"migrate:validate"},
	} {
		cmd := exec.Command(taskPath, args...)
		cmd.Dir = apiRoot
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("task %s failed: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(log)
	for _, want := range []string{
		"run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate create -ext sql -dir migrations -seq add_widgets",
		"run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database postgres://smt@example.invalid/smt up",
		"run -tags=postgres github.com/golang-migrate/migrate/v4/cmd/migrate -path migrations -database postgres://smt@example.invalid/smt version",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("fake go log missing %q:\n%s", want, logText)
		}
	}
	if !strings.Contains(logText, "GOFLAGS=-tags=postgres") {
		t.Fatalf("fake go log did not receive the PostgreSQL build tag:\n%s", logText)
	}

	sentinel := filepath.Join(t.TempDir(), "name-injection")
	maliciousName := "$(touch " + sentinel + ")"
	maliciousCmd := exec.Command(taskPath, "migrate:create", "NAME="+maliciousName)
	maliciousCmd.Dir = apiRoot
	maliciousCmd.Env = env
	maliciousOutput, maliciousErr := maliciousCmd.CombinedOutput()
	if maliciousErr != nil {
		t.Fatalf("migrate:create rejected a safely transported name: %v\n%s", maliciousErr, maliciousOutput)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("migrate:create evaluated NAME as shell code; sentinel stat error=%v", err)
	}

	failingEnv := append([]string{}, env...)
	failingEnv = append(failingEnv, "MIGRATE_FAIL=1")
	cmd := exec.Command(taskPath, "migrate:validate")
	cmd.Dir = apiRoot
	cmd.Env = failingEnv
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("migrate:validate succeeded for a failed migration:\n%s", output)
	}
	if !strings.Contains(string(output), "dirty or locked") {
		t.Fatalf("migrate:validate did not preserve migration failure output:\n%s", output)
	}
}
