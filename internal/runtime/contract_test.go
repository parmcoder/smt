package runtime

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizeProjectNameUsesSafeWorkspaceBasename(t *testing.T) {
	tests := map[string]string{
		"/tmp/My Workspace!!":             "my-workspace",
		"/tmp/Ärger/Hello.World":          "hello-world",
		"/tmp/---":                        "smt-workspace",
		"/tmp/" + strings.Repeat("a", 70): strings.Repeat("a", 63),
	}
	for input, want := range tests {
		if got := NormalizeProjectName(input); got != want {
			t.Fatalf("NormalizeProjectName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRenderComposeHonorsSelectionAndHealthDependencies(t *testing.T) {
	artifacts, err := Render(RenderOptions{
		WorkspacePath: "/tmp/Platform Workspace",
		Selection:     Selection{Web: true, API: true, Database: true},
		Ports:         PortOverrides{Web: 3100, API: 8181, Database: 55432},
	})
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(artifacts.Compose, &document); err != nil {
		t.Fatalf("rendered compose is invalid YAML: %v\n%s", err, artifacts.Compose)
	}
	if got := document["name"]; got != "platform-workspace" {
		t.Fatalf("compose name = %#v, want platform-workspace", got)
	}
	services, ok := document["services"].(map[string]any)
	if !ok {
		t.Fatalf("services = %#v, want mapping", document["services"])
	}
	for _, service := range []string{"web", "api", "database"} {
		if _, ok := services[service]; !ok {
			t.Fatalf("service %q missing from compose: %#v", service, services)
		}
	}
	if _, ok := services["mobile"]; ok {
		t.Fatal("mobile must not appear in Compose")
	}
	compose := string(artifacts.Compose)
	for _, want := range []string{
		"${WEB_PORT:-3100}:3000",
		"${API_PORT:-8181}:8080",
		"${DATABASE_PORT:-55432}:5432",
		"database-data:/var/lib/postgresql",
		"name: \"${DATABASE_VOLUME:-smt-postgres-data}\"",
		"/healthz",
		"/readyz",
		"pg_isready -h database",
		"condition: service_healthy",
		"./web-app",
		"./apis",
		"./database",
		"dockerfile: Containerfile",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("compose missing %q:\n%s", want, compose)
		}
	}
	if strings.Index(compose, "  web:") > strings.Index(compose, "  api:") || strings.Index(compose, "  api:") > strings.Index(compose, "  database:") {
		t.Fatalf("compose services are not ordered web, api, database:\n%s", compose)
	}
	if !strings.Contains(string(artifacts.EnvExample), "DATABASE_VOLUME=smt-postgres-data\n") || !strings.Contains(string(artifacts.EnvExample), "DATABASE_PASSWORD=smt-dev-password\n") {
		t.Fatalf("env example must contain the local development password example:\n%s", artifacts.EnvExample)
	}
	if strings.Contains(compose, "secret") || strings.Contains(compose, "token") || strings.Contains(compose, "password: ") {
		t.Fatalf("compose contains a credential-like value:\n%s", compose)
	}
}

func TestRenderSupportsDatabaseOnlyAndEmptySelections(t *testing.T) {
	for name, selection := range map[string]Selection{
		"database-only": {Database: true},
		"api-only":      {API: true},
		"web-only":      {Web: true},
		"api-database":  {API: true, Database: true},
		"empty":         {},
	} {
		t.Run(name, func(t *testing.T) {
			artifacts, err := Render(RenderOptions{WorkspacePath: "/tmp/workspace", Selection: selection})
			if err != nil {
				t.Fatal(err)
			}
			compose := string(artifacts.Compose)
			if strings.Contains(compose, "mobile") {
				t.Fatal("mobile must never appear in Compose")
			}
			for _, service := range []struct {
				name     string
				selected bool
			}{
				{"web", selection.Web}, {"api", selection.API}, {"database", selection.Database},
			} {
				present := strings.Contains(compose, "  "+service.name+":\n")
				if present != service.selected {
					t.Fatalf("service %q present=%t, selected=%t:\n%s", service.name, present, service.selected, compose)
				}
			}
			if name == "empty" && !strings.Contains(compose, "services: {") {
				t.Fatal("empty selection must render an empty services map")
			}
			if name != "empty" && strings.Contains(compose, "services: {") {
				t.Fatal("non-empty selection rendered an empty services map")
			}
		})
	}
}

func TestRenderIdentityAddsPinnedServicesAndOIDCContract(t *testing.T) {
	artifacts, err := Render(RenderOptions{
		WorkspacePath: "/tmp/identity-workspace",
		Selection:     Selection{API: true, Database: true, Identity: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	compose := string(artifacts.Compose)
	var document map[string]any
	if err := yaml.Unmarshal(artifacts.Compose, &document); err != nil {
		t.Fatalf("identity Compose is invalid YAML: %v\n%s", err, artifacts.Compose)
	}
	for _, service := range []string{"database", "zitadel-db-init", "zitadel", "zitadel-login", "proxy"} {
		if !strings.Contains(compose, "  "+service+":\n") {
			t.Fatalf("Compose missing identity service %q:\n%s", service, compose)
		}
	}
	for _, marker := range []string{
		"ghcr.io/zitadel/zitadel:",
		"ghcr.io/zitadel/zitadel-login:",
		"/debug/ready",
		"/debug/healthz",
		"h2c",
		"ZITADEL_EXTERNALDOMAIN",
		"ZITADEL_DATABASE_POSTGRES_DSN",
		"PGHOST: database",
		"CREATE ROLE",
		"CREATE DATABASE",
		"\\gexec",
		"OIDC_ISSUER_URL:",
		"networks: [default, zitadel]",
		"condition: service_completed_successfully",
		"OIDC_ISSUER_URL=",
		"OIDC_AUDIENCE=",
	} {
		if !strings.Contains(compose+string(artifacts.EnvExample), marker) {
			t.Fatalf("identity artifacts missing %q:\ncompose=%s\nenv=%s", marker, compose, artifacts.EnvExample)
		}
	}
	if !strings.Contains(string(artifacts.EnvExample), "ZITADEL_MASTERKEY=smt-zitadel-masterkey-local-0000\n") {
		t.Fatalf("identity env example must use the 32-byte local master key placeholder:\n%s", artifacts.EnvExample)
	}
	if strings.Index(compose, "  database:") > strings.Index(compose, "  zitadel-db-init:") || strings.Index(compose, "  zitadel-db-init:") > strings.Index(compose, "  zitadel:\n") || strings.Index(compose, "  zitadel:\n") > strings.Index(compose, "  zitadel-login:") || strings.Index(compose, "  zitadel-login:") > strings.Index(compose, "  proxy:") {
		t.Fatalf("identity services are not dependency ordered:\n%s", compose)
	}
}

func TestRenderIsByteStable(t *testing.T) {
	options := RenderOptions{WorkspacePath: "/tmp/stable", Selection: Selection{Web: true, API: true}}
	first, err := Render(options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(options)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Compose) != string(second.Compose) || string(first.EnvExample) != string(second.EnvExample) {
		t.Fatal("rendered runtime artifacts are not byte-stable")
	}
}

func TestPreflightReportsInvalidAndOccupiedPorts(t *testing.T) {
	if err := Preflight(PreflightOptions{
		Selection: Selection{Web: true},
		Ports:     PortOverrides{Web: -1},
	}); err == nil || !strings.Contains(err.Error(), "WEB_PORT") {
		t.Fatalf("invalid port error = %v, want WEB_PORT guidance", err)
	}
	if err := Preflight(PreflightOptions{
		Selection: Selection{Web: true, API: true},
		Ports:     PortOverrides{Web: 3000, API: 3000},
	}); err == nil || !strings.Contains(err.Error(), "WEB_PORT") || !strings.Contains(err.Error(), "API_PORT") {
		t.Fatalf("port collision error = %v, want both override keys", err)
	}
	if err := Preflight(PreflightOptions{
		Selection: Selection{Web: true},
		CheckPort: func(service string, port int) error { return errors.New("occupied") },
	}); err == nil || !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "3000") || !strings.Contains(err.Error(), "WEB_PORT") {
		t.Fatalf("occupied port error = %v, want service/port/action guidance", err)
	}
}

func TestPreflightReportsMissingPodmanComposePrerequisites(t *testing.T) {
	err := Preflight(PreflightOptions{
		Selection:    Selection{API: true},
		CheckPodman:  func() error { return errors.New("missing") },
		CheckCompose: func() error { return errors.New("missing") },
	})
	if err == nil || !strings.Contains(err.Error(), "Podman") || !strings.Contains(err.Error(), "install") {
		t.Fatalf("missing podman error = %v, want installation guidance", err)
	}
	if err := Preflight(PreflightOptions{
		Selection:    Selection{API: true},
		CheckPodman:  func() error { return nil },
		CheckCompose: func() error { return errors.New("missing compose") },
	}); err == nil || !strings.Contains(err.Error(), "Podman Compose") || !strings.Contains(err.Error(), "configure") {
		t.Fatalf("missing compose error = %v, want configuration guidance", err)
	}
}

func TestPreflightSkipsRuntimePrerequisitesWithoutOCISelection(t *testing.T) {
	err := Preflight(PreflightOptions{
		CheckPodman:  func() error { return errors.New("Podman must not be checked") },
		CheckCompose: func() error { return errors.New("Compose must not be checked") },
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil for an empty OCI selection", err)
	}
}
