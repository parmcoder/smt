package apply

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyGeneratesAPIRuntimeArtifacts(t *testing.T) {
	destination := applyAPIWorkspace(t, apiBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")

	containerfile := readGeneratedAPIFile(t, apiRoot, "Containerfile")
	for _, marker := range []string{
		"FROM golang:1.26.5-alpine AS builder",
		"FROM alpine:3.22",
		"CGO_ENABLED=0",
		"-trimpath",
		"-ldflags=\"-s -w\"",
		"USER 10001:10001",
		"EXPOSE 8080",
		"STOPSIGNAL SIGTERM",
		"/healthz",
		"/readyz",
		"ENTRYPOINT [\"/app/apis\"]",
	} {
		if !strings.Contains(containerfile, marker) {
			t.Fatalf("Containerfile missing %q:\n%s", marker, containerfile)
		}
	}

	taskfile := readGeneratedAPIFile(t, apiRoot, "Taskfile.yml")
	for _, task := range []string{
		"format:check",
		"lint",
		"vuln",
		"vet",
		"container:build",
		"container:build:production",
		"container:verify",
		"verify",
	} {
		if !strings.Contains(taskfile, "  "+task+":") {
			t.Fatalf("API Taskfile missing %s:\n%s", task, taskfile)
		}
	}
	for _, marker := range []string{
		"gofmt -l",
		"go tool -modfile=\"$tool_dir/go.mod\" golangci-lint run ./...",
		"go tool -modfile=\"$tool_dir/go.mod\" govulncheck ./...",
		"go mod tidy -modfile=\"$tool_dir/go.mod\"",
		"go vet ./...",
		"podman build --pull=missing",
		"podman run",
		"podman exec",
		"podman stop",
		"id -u",
		"10001",
		"Containerfile",
	} {
		if !strings.Contains(taskfile, marker) {
			t.Fatalf("API Taskfile missing runtime marker %q:\n%s", marker, taskfile)
		}
	}
	for _, forbidden := range []string{"go install", "go get", "apk add", "npm install", "docker-compose", "kubectl"} {
		if strings.Contains(strings.ToLower(containerfile+taskfile), strings.ToLower(forbidden)) {
			t.Fatalf("generated API runtime contains forbidden behavior %q", forbidden)
		}
	}

	readme := readGeneratedAPIFile(t, apiRoot, "README.md")
	for _, marker := range []string{
		"asdf install golang 1.26.5",
		"asdf current golang",
		"task lint",
		"task vuln",
		"isolated temporary module file",
		"task container:build",
		"task container:build:production",
		"task container:verify",
		"non-root",
		"8080",
		"unavailable",
		"smt-4xf.3.3.4",
	} {
		if !strings.Contains(readme, marker) {
			t.Fatalf("API README missing runtime guidance %q:\n%s", marker, readme)
		}
	}
	devBuild := taskfileTaskBlock(taskfile, "container:build")
	if !strings.Contains(devBuild, "podman build --pull=missing --format=oci --file Containerfile --tag smt-api:local .") {
		t.Fatalf("development container build does not allow missing pinned images:\n%s", devBuild)
	}
	productionBuild := taskfileTaskBlock(taskfile, "container:build:production")
	if !strings.Contains(productionBuild, "podman build --pull=never --format=oci --file Containerfile --tag \"${SMT_API_PRODUCTION_IMAGE:-smt-api:production}\" .") {
		t.Fatalf("production container build does not require preloaded pinned images:\n%s", productionBuild)
	}
}

func TestGeneratedAPIServerTemplateIsGofmtClean(t *testing.T) {
	cmd := exec.Command("gofmt", "-d")
	cmd.Stdin = strings.NewReader(apiServerGo)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gofmt failed for generated server template: %v\n%s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("generated server template is not gofmt-clean:\n%s", output)
	}
}

func TestApplyDoesNotGenerateAPIRuntimeArtifactsWithoutAPI(t *testing.T) {
	destination := applyAPIWorkspace(t, blueprintBytes())
	for _, relative := range []string{"apis/Containerfile", "apis/Taskfile.yml", "apis/README.md"} {
		if _, err := os.Stat(filepath.Join(destination, relative)); !os.IsNotExist(err) {
			t.Fatalf("API runtime artifact %s exists without API selection: %v", relative, err)
		}
	}
}

func TestApplyGeneratesAPIRuntimeArtifactsByteIdentically(t *testing.T) {
	var outputs [2]map[string][]byte
	for i := range outputs {
		destination := applyAPIWorkspace(t, apiBlueprintBytes())
		apiRoot := filepath.Join(destination, "apis")
		outputs[i] = make(map[string][]byte)
		for _, relative := range []string{"Containerfile", "Taskfile.yml", "README.md"} {
			contents, err := os.ReadFile(filepath.Join(apiRoot, relative))
			if err != nil {
				t.Fatal(err)
			}
			outputs[i][relative] = contents
		}
	}
	for relative := range outputs[0] {
		if !bytes.Equal(outputs[0][relative], outputs[1][relative]) {
			t.Fatalf("generated API runtime artifact %s is not byte-stable", relative)
		}
	}
}

func TestGeneratedAPIContainerVerifyForwardsPodmanContract(t *testing.T) {
	destination := applyAPIWorkspace(t, nonWebBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	fakeBin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "podman.log")
	fakePodman := filepath.Join(fakeBin, "podman")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$PODMAN_LOG"
case "$1" in
  build|rm|run|stop) exit 0 ;;
  wait) printf '0\n'; exit 0 ;;
  exec)
    if [ "$3" = "id" ]; then
      printf '10001\n'
    fi
    exit 0
    ;;
  inspect)
    printf 'exited\n'
    exit 0
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(fakePodman, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	taskPath := generatedTaskInstallBinary(t)
	env := append(generatedEnvironmentWithoutConfig(),
		"PATH="+fakeBin+string(os.PathListSeparator)+"/usr/bin:/bin",
		"PODMAN_LOG="+logPath,
	)
	cmd := exec.Command(taskPath, "container:verify")
	cmd.Dir = apiRoot
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		log, _ := os.ReadFile(logPath)
		t.Fatalf("generated container:verify failed with fake Podman: %v\n%s\nPodman log:\n%s", err, output, log)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("fake Podman was not invoked: %v\nTask output:\n%s\nTaskfile:\n%s", err, output, readGeneratedAPIFile(t, apiRoot, "Taskfile.yml"))
	}
	for _, marker := range []string{
		"build --pull=missing --format=oci --file Containerfile --tag smt-api:local .",
		"run --detach --name smt-api-verify --publish 127.0.0.1:18080:8080 smt-api:local",
		"exec smt-api-verify id -u",
		"exec smt-api-verify wget -q -O - http://127.0.0.1:8080/healthz",
		"exec smt-api-verify wget -q -O - http://127.0.0.1:8080/readyz",
		"stop --time 10 smt-api-verify",
		"wait smt-api-verify",
	} {
		if !strings.Contains(string(log), marker) {
			t.Fatalf("fake Podman log missing %q:\n%s", marker, log)
		}
	}
}

func TestGeneratedAPIContainerBuildReportsUnavailablePodman(t *testing.T) {
	destination := applyAPIWorkspace(t, nonWebBlueprintBytes())
	apiRoot := filepath.Join(destination, "apis")
	cmd := exec.Command(generatedTaskInstallBinary(t), "container:build")
	cmd.Dir = apiRoot
	cmd.Env = append(generatedEnvironmentWithoutConfig(), "PATH=/usr/bin:/bin")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("container:build succeeded without Podman:\n%s", output)
	}
	if !strings.Contains(string(output), "Podman is required") {
		t.Fatalf("missing actionable Podman guidance:\n%s", output)
	}
}

func generatedTaskInstallBinary(t *testing.T) string {
	t.Helper()
	asdfPath, err := exec.LookPath("asdf")
	if err != nil {
		t.Fatal(err)
	}
	taskInstall, err := exec.Command(asdfPath, "where", "task").Output()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(strings.TrimSpace(string(taskInstall)), "bin", "task")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Task install is unavailable at %s: %v", path, err)
	}
	return path
}
