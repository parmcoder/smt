package apply

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityVerificationScriptRejectsUnsafeFixtures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		want   string
	}{
		{
			name: "missing lockfile",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "web-app", "pnpm-lock.yaml")); err != nil {
					t.Fatal(err)
				}
			},
			want: "Web pnpm-lock.yaml is required",
		},
		{
			name: "root runtime user",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeSecurityFixtureFile(t, filepath.Join(root, "web-app", "Containerfile"), "FROM node:24.18.0-alpine\nUSER root\n")
			},
			want: "must not run as root",
		},
		{
			name: "privileged Compose service",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeSecurityFixtureFile(t, filepath.Join(root, "compose.yaml"), securityFixtureCompose("    privileged: true\n"))
			},
			want: "must not declare privileged services",
		},
		{
			name: "insecure no-new-privileges",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeSecurityFixtureFile(t, filepath.Join(root, "compose.yaml"), "services:\n  web:\n    image: node:24.18.0-alpine\n")
			},
			want: "no-new-privileges",
		},
		{
			name: "unpinned image",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeSecurityFixtureFile(t, filepath.Join(root, "compose.yaml"), "services:\n  web:\n    image: node:latest\n    security_opt:\n      - no-new-privileges:true\n")
			},
			want: "unpinned image reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := newSecurityWebFixture(t)
			tt.mutate(t, root)
			output, err := runSecurityVerificationScript(t, root)
			if err == nil {
				t.Fatalf("security script passed unsafe fixture:\n%s", output)
			}
			if !strings.Contains(output, tt.want) {
				t.Fatalf("security script output missing %q:\n%s", tt.want, output)
			}
		})
	}
}

func TestSecuritySecretsTaskRequestsRedactedFindings(t *testing.T) {
	root := newSecurityWebFixture(t)
	if err := os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte(rootTaskfileForSelection(true, false, false, false)), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakeGitleaks := filepath.Join(fakeBin, "gitleaks")
	if err := os.WriteFile(fakeGitleaks, []byte(`#!/bin/sh
set -eu
if [ "${1:-}" = "version" ]; then
  echo "gitleaks version 8.30.1"
  exit 0
fi
case " $* " in
  *" --redact "*) echo "finding secret=REDACTED" ;;
  *) echo "finding secret=raw-secret-value" ;;
esac
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	taskPath := generatedTaskBinary(t)
	command := exec.Command(taskPath, "security:secrets")
	command.Dir = root
	command.Env = append(generatedEnvironmentWithoutConfig(), "PATH="+fakeBin+string(os.PathListSeparator)+"/usr/bin:/bin")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("secret fixture unexpectedly passed:\n%s", output)
	}
	if !strings.Contains(string(output), "secret=REDACTED") {
		t.Fatalf("security task did not preserve the redacted finding:\n%s", output)
	}
	if strings.Contains(string(output), "raw-secret-value") {
		t.Fatalf("security task exposed the raw finding:\n%s", output)
	}
}

func newSecurityWebFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeSecurityFixtureFile(t, filepath.Join(root, "web-app", "pnpm-lock.yaml"), "lockfileVersion: '9.0'\n")
	writeSecurityFixtureFile(t, filepath.Join(root, "web-app", "Containerfile"), "FROM node:24.18.0-alpine\nUSER nextjs\n")
	writeSecurityFixtureFile(t, filepath.Join(root, "compose.yaml"), securityFixtureCompose(""))
	return root
}

func securityFixtureCompose(extra string) string {
	return "services:\n  web:\n" + extra + "    image: node:24.18.0-alpine\n    security_opt:\n      - no-new-privileges:true\n"
}

func writeSecurityFixtureFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runSecurityVerificationScript(t *testing.T, root string) (string, error) {
	t.Helper()
	script := filepath.Join(root, "scripts", "verify-security.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte(securityVerificationScript(true, false, false, false, false)), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", script)
	command.Dir = root
	output, err := command.CombinedOutput()
	return string(output), err
}
