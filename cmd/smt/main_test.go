package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/hooks"
)

func TestRunValidateMessageExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantCode   int
		wantOutput string
	}{
		{name: "valid", message: "feat(api): add a thing\n", wantCode: 0, wantOutput: "valid commit message"},
		{name: "invalid", message: "feat: add a thing\n", wantCode: 2, wantOutput: "scope is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := writeTestConfig("smt.yaml"); err != nil {
				t.Fatal(err)
			}
			file := t.TempDir() + "/message"
			if err := writeTestMessage(file, tt.message); err != nil {
				t.Fatal(err)
			}
			out, errOut := new(strings.Builder), new(strings.Builder)
			code := run([]string{"validate-message", file}, out, errOut)
			if code != tt.wantCode {
				t.Fatalf("run() code = %d, want %d; stderr=%q", code, tt.wantCode, errOut.String())
			}
			if !strings.Contains(out.String()+errOut.String(), tt.wantOutput) {
				t.Fatalf("output = %q, want substring %q", out.String()+errOut.String(), tt.wantOutput)
			}
		})
	}
}

func TestRunVerboseWritesDiagnosticsOnlyToStderr(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := writeTestConfig("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "message")
	if err := writeTestMessage(file, "feat(api): add a thing\n"); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"--verbose", "validate-message", file}, out, errOut); code != exitOK {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if got, want := out.String(), "valid commit message\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := errOut.String(); got != "level=debug msg=command finished command=validate-message exit_code=0\n" {
		t.Fatalf("stderr = %q, want deterministic debug diagnostic", got)
	}
	if strings.Contains(errOut.String(), file) {
		t.Fatalf("stderr contains command argument: %q", errOut.String())
	}
}

func TestRunNormalModeDoesNotWriteDebugDiagnostics(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeTestConfig("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "message")
	if err := writeTestMessage(file, "feat(api): add a thing\n"); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"validate-message", file}, out, errOut); code != exitOK {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want no diagnostics", errOut.String())
	}
}

func TestRunVerboseInvalidCommandPreservesUsageAndExitCode(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeTestConfig("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"--verbose", "unknown"}, out, errOut); code != exitUsage {
		t.Fatalf("run() code = %d, want %d; stderr=%q", code, exitUsage, errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
	if !strings.Contains(errOut.String(), "usage: smt") {
		t.Fatalf("stderr = %q, want usage", errOut.String())
	}
	if !strings.HasSuffix(errOut.String(), "level=debug msg=command finished command=unknown exit_code=1\n") {
		t.Fatalf("stderr = %q, want trailing invalid-command diagnostic", errOut.String())
	}
}

func writeTestConfig(path string) error {
	return os.WriteFile(path, []byte("version: 1\ncommit:\n  types: [feat]\n  scopes: [api]\nrepositories:\n  - id: root\n    path: .\n    provider: gitlab\n    project: sanovy/root\n    scope: api\n"), 0o600)
}

func writeTestMessage(path, message string) error {
	return os.WriteFile(path, []byte(message), 0o600)
}

func TestRunStatusJSONIncludesProfilesAndContractCounts(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: command
          argv: [go, test]
contracts:
  artifact:
    - id: missing
      repository: repo
      file: missing.txt
      expected: present
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"status", "--json"}, out, errOut); code != 0 {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	var got struct {
		Repositories []map[string]any `json:"repositories"`
		Profiles     []string         `json:"profiles"`
		Contracts    struct {
			Errors   int `json:"errors"`
			Warnings int `json:"warnings"`
		} `json:"contracts"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, out.String())
	}
	if len(got.Repositories) != 1 || got.Profiles[0] != "hook" || got.Contracts.Errors != 1 {
		t.Fatalf("status JSON = %#v, want repository, hook profile, and one contract error", got)
	}
}

func TestRunStatusHumanIncludesRepositoryHookState(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(testConfigYAML("gitlab", "repo", "repo")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "hooks", "commit-msg"), hooks.CommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", root, "add", "smt.yaml")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	command = exec.Command("git", "-C", root, "-c", "core.hooksPath=/dev/null", "-c", "user.name=SMT Test", "-c", "user.email=smt@example.test", "commit", "-qm", "chore(repo): fixture")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}

	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"status"}, out, errOut); code != 0 {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if want := "repo: clean hook=current"; !strings.Contains(out.String(), want) {
		t.Fatalf("status output = %q, want %q", out.String(), want)
	}
}

func TestRunDoctorDoesNotRedactOrPrintTokenValue(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(testConfigYAML("gitlab", "repo", "repo")), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := "doctor-secret-must-not-appear"
	t.Setenv("SMT_GITLAB_TOKEN", secret)
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"doctor"}, out, errOut); code != 0 {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if strings.Contains(out.String()+errOut.String(), secret) {
		t.Fatalf("doctor output contains token value: %q", out.String()+errOut.String())
	}
}

func TestRunCheckRefusesMutatingProfileWithoutPermission(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: sql-format
          argv: [pg_format]
          include: ["**/*.sql"]
          mutates_worktree: true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"check", "--profile", "hook"}, out, errOut); code == 0 || !strings.Contains(errOut.String(), "--allow-worktree-mutation") {
		t.Fatalf("check code = %d, stderr=%q, want mutation refusal", code, errOut.String())
	}
}

func TestRunContractsValidateReturnsValidationExitForEveryFinding(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
contracts:
  artifact:
    - id: first
      repository: repo
      file: first.txt
      expected: present
    - id: second
      repository: repo
      file: second.txt
      expected: present
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"contracts", "validate"}, out, errOut); code != exitValidation || !strings.Contains(out.String(), "first") || !strings.Contains(out.String(), "second") {
		t.Fatalf("code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunCIAuditUsesContractRulesAndValidationExit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
contracts:
  artifact:
    - id: audit-finding
      repository: repo
      file: missing.txt
      expected: present
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"ci", "audit"}, out, errOut); code != exitValidation || !strings.Contains(out.String(), "audit-finding") {
		t.Fatalf("code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunBumpPlanDoesNotWriteThenApplyReplaces(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
contracts:
  reference:
    - id: ci-pin
      repository: repo
      file: contract.txt
      expected: old
      replacement: new
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("contract.txt", []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"ci", "contracts", "bump", "--id", "ci-pin"}, out, errOut); code != 0 || !strings.Contains(out.String(), "old") || !strings.Contains(out.String(), "new") {
		t.Fatalf("plan code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	content, _ := os.ReadFile("contract.txt")
	if string(content) != "old\n" {
		t.Fatalf("plan changed file: %q", content)
	}
	out.Reset()
	if code := run([]string{"ci", "contracts", "bump", "--id", "ci-pin", "--apply"}, out, errOut); code != 0 {
		t.Fatalf("apply code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	content, _ = os.ReadFile("contract.txt")
	if string(content) != "new\n" {
		t.Fatalf("apply content = %q, want replacement", content)
	}
}

func testConfigYAML(provider, id, scope string) string {
	return fmt.Sprintf("version: 1\ncommit:\n  types: [feat]\n  scopes: [%s]\nrepositories:\n  - id: %s\n    path: .\n    provider: %s\n    project: sanovy/root\n    scope: %s\n", scope, id, provider, scope)
}

func initTestGit(t *testing.T, dir string) {
	t.Helper()
	command := exec.Command("git", "-C", dir, "init", "-q")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}
