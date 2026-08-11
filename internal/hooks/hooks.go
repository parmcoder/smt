// Package hooks renders and installs SMT-managed Git hooks.
package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"gopkg.in/yaml.v3"
)

const sentinel = "# SMT managed hook"

var lefthookDynamicPath = regexp.MustCompile(`(?m)^  elif (/[^\n]+) -h >/dev/null 2>&1\n  then\n    (/[^\n]+) "\$@"$`)

const (
	lefthook210CommitMsgSHA256       = "7f68eb4f4fb733475b815cb12764b9b3aac99bc2359bf31462524efe5732e3c0"
	lefthook210AssertCommitMsgSHA256 = "e2fd42e22c1e4f438dca6b547f2e2d2bd8d4b3257a2a927086eb57601bab53ea"
)

// HookStatus describes the local commit-msg hook state.
type HookStatus string

const (
	// HookAbsent means no commit-msg hook exists.
	HookAbsent HookStatus = "absent"
	// HookCurrent means the commit-msg hook contains the SMT marker.
	HookCurrent HookStatus = "current"
	// HookUnmanaged means a commit-msg hook exists without the SMT marker.
	HookUnmanaged HookStatus = "unmanaged"
)

// WorktreeInspector checks whether a configured directory is an initialized Git worktree.
type WorktreeInspector interface {
	IsWorktree(context.Context, string) (bool, error)
}

// CommandRunner executes one command with an argument array in a repository directory.
type CommandRunner interface {
	Run(context.Context, string, ...string) error
}

// InstallTarget identifies one repository whose Lefthook dispatcher can be installed.
type InstallTarget struct {
	ID  string
	Dir string
}

// InstallPlan is the complete mutation-free preflight result in configured order.
type InstallPlan struct {
	Repositories []InstallTarget
}

// InstallReport records execution progress so callers can recover manually after failure.
type InstallReport struct {
	Installed []string
	Pending   []string
	DryRun    bool
}

// ExecRunner invokes commands without a shell.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("run hook command: command is required")
	}
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Dir = dir
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", args[0], err)
	}
	return nil
}

// InspectCommitMsg reports the local commit-msg hook state without mutating it.
// It resolves both regular .git directories and gitdir pointer files.
func InspectCommitMsg(repositoryDir string) (HookStatus, error) {
	hooksDir, err := hooksDirectory(repositoryDir)
	if err != nil {
		return "", err
	}
	hookPath := filepath.Join(hooksDir, "commit-msg")
	info, err := os.Lstat(hookPath)
	if os.IsNotExist(err) {
		return HookAbsent, nil
	}
	if err != nil {
		return "", fmt.Errorf("read commit-msg hook %s: %w", repositoryDir, err)
	}
	if !info.Mode().IsRegular() {
		return HookUnmanaged, nil
	}
	contents, err := os.ReadFile(hookPath)
	if err != nil {
		return "", fmt.Errorf("read commit-msg hook %s: %w", repositoryDir, err)
	}
	if isLegacySMTCommitMsgScript(contents) || isLefthookCommitMsgDispatcher(contents) {
		return HookCurrent, nil
	}
	return HookUnmanaged, nil
}

func isLefthookCommitMsgDispatcher(contents []byte) bool {
	lines := strings.Split(string(contents), "\n")
	found := false
	for i := 0; i+2 < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "  elif /") || !strings.HasSuffix(lines[i], " -h >/dev/null 2>&1") || lines[i+1] != "  then" {
			continue
		}
		path := strings.TrimSuffix(strings.TrimPrefix(lines[i], "  elif "), " -h >/dev/null 2>&1")
		if lines[i+2] != "    "+path+" \"$@\"" {
			return false
		}
		lines[i], lines[i+2], found = "  elif <LEFTHOOK_PATH> -h >/dev/null 2>&1", "    <LEFTHOOK_PATH> \"$@\"", true
		break
	}
	if !found {
		return false
	}
	contents = []byte(strings.Join(lines, "\n"))
	match := lefthookDynamicPath.FindSubmatch(contents)
	if len(match) != 0 {
		return false
	}
	digest := sha256.Sum256(contents)
	switch hex.EncodeToString(digest[:]) {
	case lefthook210CommitMsgSHA256, lefthook210AssertCommitMsgSHA256:
		return true
	default:
		return false
	}
}

func isLegacySMTCommitMsgScript(contents []byte) bool {
	return bytes.Equal(contents, legacySMTCommitMsgScript())
}

func legacySMTCommitMsgScript() []byte {
	return []byte(`#!/bin/sh
# SMT managed hook
set -eu

workspace=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "SMT hook: cannot find Git workspace; build bin/smt and reinstall hooks" >&2
  exit 1
}

while :; do
  if [ -f "$workspace/smt.yaml" ] && [ -x "$workspace/bin/smt" ]; then
    exec "$workspace/bin/smt" validate-message "$1"
  fi
  parent=$(dirname "$workspace")
  [ "$parent" = "$workspace" ] && break
  workspace=$parent
done

echo "SMT hook: build bin/smt and reinstall hooks" >&2
exit 1
`)
}

// PlanInstall validates every configured repository before any Lefthook install runs.
func PlanInstall(ctx context.Context, workspace string, repositories []config.Repository, lookup func(string) (string, error), inspector WorktreeInspector, gitRunner git.Runner, runner CommandRunner) (InstallPlan, error) {
	if lookup == nil || inspector == nil || gitRunner == nil || runner == nil {
		return InstallPlan{}, fmt.Errorf("plan hook install: dependencies are required")
	}
	for _, name := range []string{"smt", "lefthook"} {
		if _, err := lookup(name); err != nil {
			return InstallPlan{}, fmt.Errorf("plan hook install: required executable %q is unavailable", name)
		}
	}
	plan := InstallPlan{Repositories: make([]InstallTarget, 0, len(repositories))}
	var firstErr error
	for _, repository := range repositories {
		dir := filepath.Join(workspace, repository.Path)
		initialized, err := inspector.IsWorktree(ctx, dir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: inspect worktree: %w", repository.ID, err)
			}
			continue
		}
		if !initialized {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: repository is not initialized", repository.ID)
			}
			continue
		}
		configured, err := coreHooksPathConfigured(ctx, gitRunner, dir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: inspect core.hooksPath failed", repository.ID)
			}
			continue
		}
		if configured {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: core.hooksPath is configured", repository.ID)
			}
			continue
		}
		if err := validateLefthookConfig(dir); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: %w", repository.ID, err)
			}
			continue
		}
		if err := runner.Run(ctx, dir, "lefthook", "validate"); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: lefthook validation failed", repository.ID)
			}
			continue
		}
		status, err := InspectCommitMsg(dir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: inspect commit-msg hook: %w", repository.ID, err)
			}
			continue
		}
		if status == HookUnmanaged {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: commit-msg hook is unmanaged", repository.ID)
			}
			continue
		}
		collision, err := legacyMigrationBackupCollision(dir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: inspect legacy hook migration state failed", repository.ID)
			}
			continue
		}
		if collision {
			if firstErr == nil {
				firstErr = fmt.Errorf("preflight repository %s: legacy hook migration collision", repository.ID)
			}
			continue
		}
		plan.Repositories = append(plan.Repositories, InstallTarget{ID: repository.ID, Dir: dir})
	}
	if firstErr != nil {
		return InstallPlan{}, firstErr
	}
	return plan, nil
}

func coreHooksPathConfigured(ctx context.Context, runner git.Runner, repositoryDir string) (bool, error) {
	result, err := runner.Run(ctx, repositoryDir, "config", "--get", "core.hooksPath")
	value := strings.TrimSuffix(result.Stdout, "\n")
	if result.ExitCode == 1 && value == "" && result.Stderr == "" {
		return false, nil
	}
	if err != nil || result.ExitCode != 0 {
		return false, fmt.Errorf("git config lookup failed")
	}
	return value != "", nil
}

func legacyMigrationBackupCollision(repositoryDir string) (bool, error) {
	hooksDir, err := hooksDirectory(repositoryDir)
	if err != nil {
		return false, err
	}
	hookPath := filepath.Join(hooksDir, "commit-msg")
	info, err := os.Lstat(hookPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	contents, err := os.ReadFile(hookPath)
	if err != nil {
		return false, err
	}
	if !isLegacySMTCommitMsgScript(contents) {
		return false, nil
	}
	_, err = os.Lstat(hookPath + ".old")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func validateLefthookConfig(repositoryDir string) error {
	contents, err := os.ReadFile(filepath.Join(repositoryDir, "lefthook.yml"))
	if err != nil {
		return fmt.Errorf("read lefthook.yml: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse lefthook.yml: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("parse lefthook.yml: top-level mapping is required")
	}
	for i := 0; i+1 < len(document.Content[0].Content); i += 2 {
		if document.Content[0].Content[i].Value == "commit-msg" && document.Content[0].Content[i+1].Kind == yaml.MappingNode {
			return nil
		}
	}
	return fmt.Errorf("lefthook.yml commit-msg configuration is required")
}

// ExecuteInstall installs Lefthook dispatchers root-first without rollback.
func ExecuteInstall(ctx context.Context, plan InstallPlan, runner CommandRunner, dryRun bool) (InstallReport, error) {
	report := InstallReport{DryRun: dryRun, Pending: make([]string, 0, len(plan.Repositories))}
	for _, repository := range plan.Repositories {
		report.Pending = append(report.Pending, repository.ID)
	}
	if dryRun {
		return report, nil
	}
	if runner == nil {
		return report, fmt.Errorf("execute hook install: runner is required")
	}
	for i, repository := range plan.Repositories {
		if err := runner.Run(ctx, repository.Dir, "lefthook", "install", "commit-msg"); err != nil {
			report.Pending = append([]string(nil), report.Pending[i:]...)
			return report, fmt.Errorf("install repository %s: %w", repository.ID, err)
		}
		report.Installed = append(report.Installed, repository.ID)
	}
	report.Pending = nil
	return report, nil
}

func hooksDirectory(repositoryDir string) (string, error) {
	gitPath := filepath.Join(repositoryDir, ".git")
	info, err := os.Stat(gitPath)
	if os.IsNotExist(err) || (err == nil && info.IsDir()) {
		return filepath.Join(gitPath, "hooks"), nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect Git directory %s: %w", gitPath, err)
	}
	// Submodules and linked worktrees use a .git file that points at the real gitdir.
	contents, err := os.ReadFile(gitPath)
	if err != nil {
		return "", fmt.Errorf("read Git directory pointer %s: %w", gitPath, err)
	}
	const prefix = "gitdir: "
	gitDir := strings.TrimSpace(strings.TrimPrefix(string(contents), prefix))
	if gitDir == "" || string(contents) == gitDir {
		return "", fmt.Errorf("read Git directory pointer %s: invalid gitdir", gitPath)
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repositoryDir, gitDir)
	}
	return filepath.Join(gitDir, "hooks"), nil
}
