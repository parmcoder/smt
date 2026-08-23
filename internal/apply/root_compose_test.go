package apply

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGeneratesRootComposeTaskfileWithExplicitEnvFile(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, databaseBlueprintBytes())

	taskfile, err := os.ReadFile(filepath.Join(destination, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(taskfile)
	for _, marker := range []string{
		"compose:config:",
		"compose:build:",
		"compose:up:",
		"compose:down:",
		"compose:ps:",
		"--env-file \"{{.ROOT_DIR}}/.env\"",
		"-f \"{{.ROOT_DIR}}/compose.yaml\"",
		"DATABASE_PASSWORD",
		"copy .env.example to .env",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("root Compose Taskfile missing %q:\n%s", marker, text)
		}
	}
	if strings.Contains(text, "dotenv:") {
		t.Fatalf("root Compose Taskfile must pass .env explicitly rather than rely on Task dotenv loading:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(destination, ".env")); !os.IsNotExist(err) {
		t.Fatalf("Apply generated a root .env: %v", err)
	}
	readme, err := os.ReadFile(filepath.Join(destination, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "task compose:up") {
		t.Fatalf("root README is missing the Compose entrypoint:\n%s", readme)
	}
	envExample, err := os.ReadFile(filepath.Join(destination, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(envExample), "DATABASE_PASSWORD=smt-dev-password\n") {
		t.Fatalf("root env example is missing the local development password:\n%s", envExample)
	}
}

func TestRootComposeTasksMergeWithE2ECoordinator(t *testing.T) {
	e2eTaskfile := e2eOrchestrationFiles(true, true)["Taskfile.yml"]
	merged := addRootComposeTasks(e2eTaskfile, true)
	for _, marker := range []string{"compose:up:", "web:", "mobile:", "verify:", "--env-file \"{{.ROOT_DIR}}/.env\""} {
		if !strings.Contains(merged, marker) {
			t.Fatalf("merged root Taskfile missing %q:\n%s", marker, merged)
		}
	}
	if strings.Count(merged, "version: '3'") != 1 || strings.Count(merged, "tasks:\n") != 1 {
		t.Fatalf("merged root Taskfile duplicated YAML document sections:\n%s", merged)
	}
}

func TestGeneratedRootComposeUpUsesRootEnvFileAndGuardsMissingEnv(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, databaseBlueprintBytes())
	if err := os.WriteFile(filepath.Join(destination, ".env"), []byte("DATABASE_PASSWORD=local-only\nDATABASE_VOLUME=smt-test-volume\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "podman.log")
	fakePodman := filepath.Join(fakeBin, "podman")
	if err := os.WriteFile(fakePodman, []byte(`#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$PODMAN_LOG"
test "$1" = compose
exit 0
`), 0o755); err != nil {
		t.Fatal(err)
	}
	taskPath := generatedTaskInstallBinary(t)
	env := append(generatedEnvironmentWithoutConfig(),
		"PATH="+fakeBin+string(os.PathListSeparator)+"/usr/bin:/bin",
		"PODMAN_LOG="+logPath,
	)
	cmd := exec.Command(taskPath, "compose:up")
	cmd.Dir = destination
	cmd.Env = env
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated compose:up failed with fake Podman: %v\n%s", err, output)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"compose --env-file ",
		"/.env",
		"-f ",
		"/compose.yaml up -d",
	} {
		if !strings.Contains(string(log), marker) {
			t.Fatalf("fake Podman log missing %q:\n%s", marker, log)
		}
	}

	if err := os.Remove(filepath.Join(destination, ".env")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(taskPath, "compose:up")
	cmd.Dir = destination
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "root .env is required") {
		t.Fatalf("compose:up without root .env output=%q err=%v", output, err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("Podman was invoked before the missing .env guard: %v", err)
	}
}
