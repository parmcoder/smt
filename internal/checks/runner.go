// Package checks dispatches configured repository checks without a shell.
package checks

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/parmcoder/smt/internal/config"
)

// Executor runs one argument-array command from a repository directory.
type Executor interface {
	Run(context.Context, string, []string, string) error
}

// Run dispatches configured checks for a repository and stops at the first error.
func Run(ctx context.Context, executor Executor, repository config.Repository, changedFiles []string, dryRun bool) error {
	// Dry-run validates the planned dispatch without invoking commands or formatters.
	if dryRun {
		return nil
	}
	if executor == nil {
		return fmt.Errorf("run checks for repository %s: executor is required", repository.ID)
	}
	return runChecks(ctx, executor, repository, repository.Checks, changedFiles, true)
}

// RunProfile dispatches one named check profile and enforces its mutation policy.
func RunProfile(ctx context.Context, executor Executor, repository config.Repository, profile string, changedFiles []string, allowWorktreeMutation, dryRun bool) error {
	checks, ok := repository.Profiles[profile]
	if !ok {
		return fmt.Errorf("run checks for repository %s: unknown check profile %q", repository.ID, profile)
	}
	if dryRun {
		return nil
	}
	if executor == nil {
		return fmt.Errorf("run checks for repository %s: executor is required", repository.ID)
	}
	for _, check := range checks {
		if (check.MutatesWorktree || check.Kind == "sql-format") && !allowWorktreeMutation {
			return fmt.Errorf("run %s check for repository %s: check mutates the worktree; rerun with --allow-worktree-mutation", check.Kind, repository.ID)
		}
	}
	return runChecks(ctx, executor, repository, checks, changedFiles, true)
}

func runChecks(ctx context.Context, executor Executor, repository config.Repository, checks []config.Check, changedFiles []string, honorInclude bool) error {
	for _, check := range checks {
		switch check.Kind {
		case "command":
			if err := executor.Run(ctx, repository.Path, append([]string(nil), check.Argv...), repository.ID); err != nil {
				return fmt.Errorf("run command check for repository %s: %w", repository.ID, err)
			}
		case "sql-format":
			for _, file := range changedFiles {
				if filepath.Ext(file) != ".sql" {
					continue
				}
				if honorInclude && !matchesAny(check.Include, file) {
					continue
				}
				// Formatting is intentionally fixed to pg_format rather than a shell-configured command.
				argv := []string{"pg_format", "-i", file}
				if err := executor.Run(ctx, repository.Path, argv, repository.ID); err != nil {
					return fmt.Errorf("format SQL file %s for repository %s: %w", file, repository.ID, err)
				}
			}
		default:
			return fmt.Errorf("run checks for repository %s: unsupported check kind %q", repository.ID, check.Kind)
		}
	}
	return nil
}

func matchesAny(patterns []string, file string) bool {
	file = filepath.ToSlash(file)
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(pattern)
		if pattern == file {
			return true
		}
		if strings.HasPrefix(pattern, "**/") {
			pattern = strings.TrimPrefix(pattern, "**/")
			if ok, _ := path.Match(pattern, file); ok {
				return true
			}
			if ok, _ := path.Match(pattern, filepath.Base(file)); ok {
				return true
			}
		}
		if ok, _ := path.Match(pattern, file); ok {
			return true
		}
	}
	return false
}
