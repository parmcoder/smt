package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndValidate(t *testing.T) {
	valid := `version: 1
commit:
  types: [feat, fix]
  scopes: [api, web]
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: api
    checks:
      - kind: command
        argv: [task, verify]
  - id: web
    path: web
    provider: github
    project: sanovy/web
    scope: web
    checks:
      - kind: sql-format
        argv: [pg_format]
        include: ["**/*.sql"]
`

	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "valid", yaml: valid},
		{name: "wrong version", yaml: strings.Replace(valid, "version: 1", "version: 2", 1), want: "version must be 1"},
		{name: "duplicate id", yaml: strings.Replace(valid, "id: web", "id: root", 1), want: "duplicate repository id"},
		{name: "duplicate path", yaml: strings.Replace(valid, "path: web", "path: .", 1), want: "duplicate repository path"},
		{name: "duplicate cleaned path", yaml: strings.Replace(valid, "path: web", "path: ./", 1), want: "duplicate repository path"},
		{name: "duplicate project", yaml: strings.Replace(valid, "project: sanovy/web", "project: sanovy/root", 1), want: "duplicate repository project"},
		{name: "duplicate scope", yaml: strings.Replace(valid, "scope: web", "scope: api", 1), want: "duplicate repository scope"},
		{name: "missing root", yaml: strings.Replace(valid, "    path: .\n", "    path: root\n", 1), want: "exactly one root repository"},
		{name: "path escape", yaml: strings.Replace(valid, "path: web", "path: ../web", 1), want: "must remain inside workspace"},
		{name: "unknown provider", yaml: strings.Replace(valid, "provider: github", "provider: bitbucket", 1), want: "provider must be gitlab or github"},
		{name: "scope not declared", yaml: strings.Replace(valid, "scope: web", "scope: other", 1), want: "scope \"other\" is not declared"},
		{name: "empty command", yaml: strings.Replace(valid, "argv: [task, verify]", "argv: []", 1), want: "command check argv must not be empty"},
		{name: "empty command argument", yaml: strings.Replace(valid, "argv: [task, verify]", "argv: [task, \"\"]", 1), want: "check argv contains an empty argument"},
		{name: "empty sql formatter", yaml: strings.Replace(valid, "argv: [pg_format]", "argv: []", 1), want: "sql-format check argv must not be empty"},
		{name: "sql include missing", yaml: strings.Replace(valid, "        include: [\"**/*.sql\"]\n", "", 1), want: "sql-format check include must not be empty"},
		{name: "empty sql include", yaml: strings.Replace(valid, "include: [\"**/*.sql\"]", "include: [\"\"]", 1), want: "sql-format check include contains an empty pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "smt.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Load() error = %v", err)
				}
				if got.Version != 1 {
					t.Fatalf("Version = %d, want 1", got.Version)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadAllowsRepeatedProvider(t *testing.T) {
	yaml := `version: 1
commit:
  types: [feat]
  scopes: [api, web]
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: api
  - id: web
    path: web
    provider: gitlab
    project: sanovy/web
    scope: web
`
	dir := t.TempDir()
	path := filepath.Join(dir, "smt.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v, want repeated providers to be valid", err)
	}
}

func TestLoadNamedProfilesAndContracts(t *testing.T) {
	yaml := `version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: command
          argv: [go, test, ./...]
      submit:
        - kind: sql-format
          argv: [pg_format]
          include: ["migrations/**/*.sql"]
          mutates_worktree: true
contracts:
  reference:
    - id: api-contract
      repository: root
      file: openapi.yaml
      expected: committed
      replacement: generated
      severity: warn
  migration-coverage:
    - id: migration-contract
      repository: root
      file: migrations/001.sql
      expected: applied
      source: schema.sql
  artifact:
    - id: artifact-contract
      repository: root
      file: dist/app.js
      expected: present
`
	path := writeConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	profiles := cfg.Repositories[0].Profiles
	if len(profiles["hook"]) != 1 || len(profiles["submit"]) != 1 {
		t.Fatalf("Profiles = %#v, want hook and submit profiles", profiles)
	}
	if !profiles["submit"][0].MutatesWorktree {
		t.Fatal("submit SQL check mutation declaration = false, want true")
	}
	if got := cfg.Contracts.Reference[0].Severity; got != "warn" {
		t.Fatalf("reference severity = %q, want warn", got)
	}
	if got := cfg.Contracts.Artifact[0].Severity; got != "error" {
		t.Fatalf("artifact severity = %q, want default error", got)
	}
}

func TestValidateRejectsInvalidContracts(t *testing.T) {
	base := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: root, path: ., provider: gitlab, project: sanovy/root, scope: repo}
contracts:
  artifact:
    - {id: artifact, repository: missing, file: ../dist/app.js, expected: present}
`
	for name, want := range map[string]string{
		"unknown repository": "contract repository",
		"escaping file":      "contract file",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, strings.Replace(base, "repository: missing", "repository: root", 1))
			if name == "unknown repository" {
				path = writeConfig(t, base)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Load() error = %v, want substring %q", err, want)
			}
		})
	}
}

func TestValidateRejectsMutationOnUnknownCheckKind(t *testing.T) {
	yaml := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - {kind: unknown, argv: [echo, ok], mutates_worktree: true}
`
	_, err := Load(writeConfig(t, yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown check kind") {
		t.Fatalf("Load() error = %v, want unknown check kind", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "smt.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
