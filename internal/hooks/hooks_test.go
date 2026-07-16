package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parmcoder/smt/internal/config"
)

func TestCommitMsgScriptDiscoversWorkspaceAndExplainsMissingBinary(t *testing.T) {
	script := string(CommitMsgScript())
	for _, fragment := range []string{
		"# SMT managed hook",
		"git rev-parse --show-toplevel",
		"smt.yaml",
		"-x \"$workspace/bin/smt\"",
		"exec \"$workspace/bin/smt\" validate-message \"$1\"",
		"build bin/smt and reinstall hooks",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("CommitMsgScript() missing %q:\n%s", fragment, script)
		}
	}
}

func TestInstallScopesHooksBacksUpUnmanagedFilesAndIsIdempotent(t *testing.T) {
	workspace := t.TempDir()
	repositories := []config.Repository{
		{ID: "repo", Path: "."},
		{ID: "api", Path: "apis", Checks: []config.Check{{Kind: "command", Argv: []string{"task", "verify"}}}},
		{ID: "infra", Path: "devops"},
	}
	apiHooks := filepath.Join(workspace, "apis", ".git", "hooks")
	if err := os.MkdirAll(apiHooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiHooks, "commit-msg"), []byte("#!/bin/sh\necho custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiHooks, "pre-commit"), []byte("#!/bin/sh\necho custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, time.July, 16, 12, 34, 56, 0, time.UTC) }

	results, err := Install(workspace, repositories, now)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("results = %#v, want four installed hooks", results)
	}
	backupSuffix := ".smt-backup.20260716T123456Z"
	for _, hook := range []string{"commit-msg", "pre-commit"} {
		backup := filepath.Join(apiHooks, hook+backupSuffix)
		if _, err := os.Stat(backup); err != nil {
			t.Fatalf("backup %s: %v", backup, err)
		}
		installed := filepath.Join(apiHooks, hook)
		info, err := os.Stat(installed)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode = %o, want 755", info.Mode().Perm())
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("root pre-commit exists or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "devops", ".git", "hooks", "pre-commit")); !os.IsNotExist(err) {
		t.Fatalf("devops pre-commit exists or stat failed: %v", err)
	}

	before, err := os.ReadFile(filepath.Join(apiHooks, "commit-msg"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(workspace, repositories, now)
	if err != nil {
		t.Fatalf("second Install() error = %v", err)
	}
	for _, result := range second {
		if result.Backup != "" {
			t.Fatalf("idempotent reinstall made backup: %#v", second)
		}
	}
	after, err := os.ReadFile(filepath.Join(apiHooks, "commit-msg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("managed commit-msg hook changed on idempotent reinstall")
	}
}

func TestPreCommitScriptIncludesRepositoryID(t *testing.T) {
	script := string(PreCommitScript("api"))
	if !strings.Contains(script, "run-checks api") || !strings.Contains(script, "# SMT managed hook") {
		t.Fatalf("PreCommitScript() = %q", script)
	}
}

func TestInspectCommitMsgReportsHookStates(t *testing.T) {
	workspace := t.TempDir()
	for _, repository := range []string{"current", "unmanaged"} {
		if err := os.MkdirAll(filepath.Join(workspace, repository, ".git", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "current", ".git", "hooks", "commit-msg"), CommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "unmanaged", ".git", "hooks", "commit-msg"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		repository string
		want       HookStatus
	}{
		{repository: "absent", want: HookAbsent},
		{repository: "current", want: HookCurrent},
		{repository: "unmanaged", want: HookUnmanaged},
	}
	for _, test := range tests {
		got, err := InspectCommitMsg(filepath.Join(workspace, test.repository))
		if err != nil {
			t.Fatalf("InspectCommitMsg(%q) error = %v", test.repository, err)
		}
		if got != test.want {
			t.Errorf("InspectCommitMsg(%q) = %q, want %q", test.repository, got, test.want)
		}
	}
}

func TestInspectCommitMsgResolvesGitdirPointer(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "repository")
	gitDir := filepath.Join(workspace, "real-gitdir")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".git"), []byte("gitdir: ../real-gitdir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "hooks", "commit-msg"), CommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := InspectCommitMsg(repository)
	if err != nil {
		t.Fatalf("InspectCommitMsg() error = %v", err)
	}
	if got != HookCurrent {
		t.Fatalf("InspectCommitMsg() = %q, want %q", got, HookCurrent)
	}
}

func TestInspectCommitMsgDoesNotMutateHook(t *testing.T) {
	repository := t.TempDir()
	hooksDir := filepath.Join(repository, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "commit-msg")
	contents := []byte("#!/bin/sh\necho custom\n")
	if err := os.WriteFile(hookPath, contents, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := InspectCommitMsg(repository); err != nil {
		t.Fatalf("InspectCommitMsg() error = %v", err)
	}
	after, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(contents) {
		t.Fatalf("hook contents changed: %q", after)
	}
}
