package apply

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGeneratesDatabaseRuntimeAndReadinessAssets(t *testing.T) {
	destination := t.TempDir()
	applyWorkspace(t, filepath.Join(destination, "workspace"), databaseBlueprintBytes())
	database := filepath.Join(destination, "workspace", "database")

	for relative, markers := range map[string][]string{
		"Containerfile": {
			"FROM postgres:18-alpine",
			"ENV POSTGRES_DB=smt",
			"ENV POSTGRES_USER=smt",
			"EXPOSE 5432",
			"VOLUME [\"/var/lib/postgresql\"]",
			"HEALTHCHECK",
			"pg_isready",
		},
		"Taskfile.yml": {
			"dotenv: ['.env']",
			"podman build --pull=missing --format=docker",
			"POSTGRES_PASSWORD is required",
			"--volume \"${volume}:/var/lib/postgresql\"",
			"pg_isready",
			"psql --no-password",
			"ON_ERROR_STOP=1",
			"psql:",
			"task: stop",
		},
		".env.example": {
			"POSTGRES_DB=smt\n",
			"POSTGRES_USER=smt\n",
			"POSTGRES_PASSWORD=\n",
			"DATABASE_VOLUME=smt-postgres-data\n",
		},
		"README.md": {
			"PostgreSQL 18",
			"pg_isready",
			"fail-fast",
			"psql query",
			"does not contain application schema",
		},
	} {
		contents, err := os.ReadFile(filepath.Join(database, relative))
		if err != nil {
			t.Fatalf("read generated database file %s: %v", relative, err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(contents), marker) {
				t.Errorf("generated database file %s missing %q:\n%s", relative, marker, contents)
			}
		}
	}
}

func TestApplyDatabaseRuntimeDoesNotGenerateAPIOrMigrationAssets(t *testing.T) {
	destination := t.TempDir()
	applyWorkspace(t, filepath.Join(destination, "workspace"), databaseBlueprintBytes())
	database := filepath.Join(destination, "workspace", "database")

	for _, relative := range []string{
		"main.go",
		"go.mod",
		"Containerfile.api",
		filepath.Join("internal", "server"),
		"migrations",
		"schema",
	} {
		if _, err := os.Lstat(filepath.Join(database, relative)); !os.IsNotExist(err) {
			t.Fatalf("database output contains API/schema asset %s: %v", relative, err)
		}
	}

	for _, relative := range []string{"apis", "web-app", "mobile-app"} {
		if _, err := os.Lstat(filepath.Join(destination, "workspace", relative)); !os.IsNotExist(err) {
			t.Fatalf("database-only workspace contains unrelated component %s: %v", relative, err)
		}
	}

	for relative, contents := range databaseRuntimeFiles() {
		text := strings.ToLower(contents)
		if strings.Contains(text, "password=secret") || strings.Contains(text, "password: secret") {
			t.Errorf("generated database file %s contains an embedded credential", relative)
		}
	}
}

func TestApplyDatabaseRuntimeIsDeterministic(t *testing.T) {
	firstParent := t.TempDir()
	secondParent := t.TempDir()
	first := filepath.Join(firstParent, "workspace")
	second := filepath.Join(secondParent, "workspace")
	applyWorkspace(t, first, databaseBlueprintBytes())
	applyWorkspace(t, second, databaseBlueprintBytes())

	for relative := range databaseRuntimeFiles() {
		left, err := os.ReadFile(filepath.Join(first, "database", relative))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, "database", relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("database runtime file %s differs across fresh Apply destinations", relative)
		}
	}
}

func TestGeneratedDatabaseTaskfileParsesWithTask(t *testing.T) {
	destination := t.TempDir()
	applyWorkspace(t, filepath.Join(destination, "workspace"), databaseBlueprintBytes())
	database := filepath.Join(destination, "workspace", "database")

	command := exec.Command("task", "--list-all")
	command.Dir = database
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Database Taskfile does not parse: %v\n%s", err, output)
	}
	for _, want := range []string{"build", "run", "ready", "psql", "diagnose", "stop", "verify"} {
		if !strings.Contains(string(output), want) {
			t.Errorf("task --list-all output missing %q:\n%s", want, output)
		}
	}
}

func databaseBlueprintBytes() []byte {
	return []byte(`version: 1
provenance: {tool: smt, smt_version: v0.1.0, template_set_version: v1}
workspace: {ai_assist: codex, stack: {database: postgresql}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, database]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: database, path: database, component: database, technology: postgresql, scope: database, modules: [database], remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}
