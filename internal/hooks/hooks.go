// Package hooks renders and installs SMT-managed Git hooks.
package hooks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/parmcoder/smt/internal/config"
)

const sentinel = "# SMT managed hook"

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

// InstallResult describes one installed hook and any backup created for it.
type InstallResult struct {
	Repository string
	Hook       string
	Backup     string
}

// CommitMsgScript returns the managed commit-msg hook source.
func CommitMsgScript() []byte {
	return hookScript(`validate-message "$1"`)
}

// PreCommitScript returns the managed pre-commit hook source for repositoryID.
func PreCommitScript(repositoryID string) []byte {
	return hookScript("run-checks " + repositoryID)
}

// InspectCommitMsg reports the local commit-msg hook state without mutating it.
// It resolves both regular .git directories and gitdir pointer files.
func InspectCommitMsg(repositoryDir string) (HookStatus, error) {
	hooksDir, err := hooksDirectory(repositoryDir)
	if err != nil {
		return "", err
	}
	contents, err := os.ReadFile(filepath.Join(hooksDir, "commit-msg"))
	if os.IsNotExist(err) {
		return HookAbsent, nil
	}
	if err != nil {
		return "", fmt.Errorf("read commit-msg hook %s: %w", repositoryDir, err)
	}
	if bytes.Contains(contents, []byte(sentinel)) {
		return HookCurrent, nil
	}
	return HookUnmanaged, nil
}

// Install writes managed hooks for all configured repositories.
func Install(workspace string, repositories []config.Repository, now func() time.Time) ([]InstallResult, error) {
	if now == nil {
		return nil, fmt.Errorf("install hooks: clock is required")
	}
	results := make([]InstallResult, 0, len(repositories)*2)
	for _, repository := range repositories {
		hooksDir, err := hooksDirectory(filepath.Join(workspace, repository.Path))
		if err != nil {
			return nil, err
		}
		result, err := installHook(hooksDir, repository.ID, "commit-msg", CommitMsgScript(), now)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
		if len(repository.Checks) == 0 {
			continue
		}
		result, err = installHook(hooksDir, repository.ID, "pre-commit", PreCommitScript(repository.ID), now)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
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

func hookScript(command string) []byte {
	return []byte(`#!/bin/sh
` + sentinel + `
set -eu

workspace=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "SMT hook: cannot find Git workspace; build bin/smt and reinstall hooks" >&2
  exit 1
}

while :; do
  if [ -f "$workspace/smt.yaml" ] && [ -x "$workspace/bin/smt" ]; then
    exec "$workspace/bin/smt" ` + command + `
  fi
  parent=$(dirname "$workspace")
  [ "$parent" = "$workspace" ] && break
  workspace=$parent
done

echo "SMT hook: build bin/smt and reinstall hooks" >&2
exit 1
`)
}

func installHook(hooksDir, repositoryID, hook string, script []byte, now func() time.Time) (InstallResult, error) {
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create hooks directory %s: %w", hooksDir, err)
	}
	target := filepath.Join(hooksDir, hook)
	backup := ""
	existing, err := os.ReadFile(target)
	// Sentinel-bearing hooks are already SMT-owned and can be replaced idempotently.
	if err == nil && !bytes.Contains(existing, []byte(sentinel)) {
		backup = target + ".smt-backup." + now().UTC().Format("20060102T150405Z")
		if _, err := os.Lstat(backup); err == nil {
			return InstallResult{}, fmt.Errorf("backup already exists: %s", backup)
		} else if !os.IsNotExist(err) {
			return InstallResult{}, fmt.Errorf("inspect hook backup %s: %w", backup, err)
		}
		// Preserve an unmanaged hook before replacing it with SMT-managed content.
		if err := os.Rename(target, backup); err != nil {
			return InstallResult{}, fmt.Errorf("backup hook %s: %w", target, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return InstallResult{}, fmt.Errorf("read hook %s: %w", target, err)
	}
	if err := os.WriteFile(target, script, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("write hook %s: %w", target, err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("make hook executable %s: %w", target, err)
	}
	return InstallResult{Repository: repositoryID, Hook: hook, Backup: backup}, nil
}
