package git

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Repository identifies a configured Git repository.
type Repository struct {
	ID     string
	Dir    string
	IsRoot bool
}

// State describes the current local state of a Git repository.
type State struct {
	Branch       string
	Detached     bool
	Dirty        bool
	Initialized  bool
	ChangedFiles []string
}

// CommitMessage is one commit SHA and its complete message.
type CommitMessage struct {
	SHA     string
	Message string
}

// Inspector checks whether directories are initialized Git worktrees.
type Inspector struct {
	Runner Runner
}

// IsWorktree reports whether dir is inside an initialized Git worktree.
func (i Inspector) IsWorktree(ctx context.Context, dir string) (bool, error) {
	if i.Runner == nil {
		return false, fmt.Errorf("inspect worktree %s: runner is required", dir)
	}
	result, err := i.Runner.Run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if hasNonZeroExit(result) {
		// Git uses a non-zero exit status when the directory is not a worktree.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect worktree %s: %w", dir, err)
	}
	return strings.TrimSpace(result.Stdout) == "true", nil
}

// Inspect returns initialized, dirty, branch, detached, and status-file state.
func Inspect(ctx context.Context, runner Runner, repository Repository) (State, error) {
	if runner == nil {
		return State{}, fmt.Errorf("inspect repository %s: runner is required", repository.ID)
	}
	state := State{}
	result, err := runner.Run(ctx, repository.Dir, "rev-parse", "--is-inside-work-tree")
	if hasNonZeroExit(result) {
		// A non-zero rev-parse result means this configured path is uninitialized.
		return state, nil
	}
	if err != nil {
		return State{}, repositoryError("inspect worktree", repository, err)
	}
	if strings.TrimSpace(result.Stdout) != "true" {
		return state, nil
	}
	state.Initialized = true

	result, err = runner.Run(ctx, repository.Dir, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || hasNonZeroExit(result) {
		return State{}, repositoryError("inspect status", repository, commandError(result, err))
	}
	state.ChangedFiles = porcelainFiles(result.Stdout)
	state.Dirty = len(state.ChangedFiles) > 0

	result, err = runner.Run(ctx, repository.Dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if hasNonZeroExit(result) {
		// symbolic-ref exits non-zero for detached HEAD, which is valid state.
		state.Detached = true
		return state, nil
	}
	if err != nil {
		return State{}, repositoryError("inspect branch", repository, err)
	}
	state.Branch = strings.TrimSpace(result.Stdout)
	return state, nil
}

// ChangedFiles returns the unique non-ignored changed paths for repository.
func ChangedFiles(ctx context.Context, runner Runner, repository Repository) ([]string, error) {
	if runner == nil {
		return nil, fmt.Errorf("changed files for repository %s: runner is required", repository.ID)
	}
	commands := [][]string{
		{"diff", "--name-only", "--diff-filter=ACMRTUXB"},
		{"diff", "--cached", "--name-only", "--diff-filter=ACMRTUXB"},
		{"ls-files", "--others", "--exclude-standard"},
	}
	files := make(map[string]struct{})
	for _, args := range commands {
		result, err := runner.Run(ctx, repository.Dir, args...)
		if err != nil || hasNonZeroExit(result) {
			return nil, repositoryError("list changed files", repository, commandError(result, err))
		}
		for _, file := range outputLines(result.Stdout) {
			files[file] = struct{}{}
		}
	}
	return sortedFiles(files), nil
}

// CommitMessages returns full commit messages from the requested Git range.
func CommitMessages(ctx context.Context, runner Runner, repository Repository, from, to string) ([]CommitMessage, error) {
	if runner == nil {
		return nil, fmt.Errorf("commit messages for repository %s: runner is required", repository.ID)
	}
	rangeArg := from + ".." + to
	result, err := runner.Run(ctx, repository.Dir, "log", "--format=%H%x00%B%x00", rangeArg, "--")
	if err != nil || hasNonZeroExit(result) {
		return nil, repositoryError("read commit messages", repository, commandError(result, err))
	}
	parts := strings.Split(result.Stdout, "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return nil, nil
	}
	if len(parts) < 3 || parts[len(parts)-1] != "" || len(parts)%2 == 0 {
		return nil, fmt.Errorf("read commit messages for repository %s: malformed Git log output", repository.ID)
	}
	messages := make([]CommitMessage, 0, (len(parts)-1)/2)
	for index := 0; index < len(parts)-1; index += 2 {
		if parts[index] == "" {
			return nil, fmt.Errorf("read commit messages for repository %s: malformed Git log output", repository.ID)
		}
		messages = append(messages, CommitMessage{
			SHA:     parts[index],
			Message: strings.TrimSuffix(parts[index+1], "\n"),
		})
	}
	return messages, nil
}

func hasNonZeroExit(result Result) bool {
	return result.ExitCode > 0
}

func commandError(result Result, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("git exited with status %d", result.ExitCode)
}

func repositoryError(operation string, repository Repository, err error) error {
	return fmt.Errorf("%s for repository %s at %s: %w", operation, repository.ID, repository.Dir, err)
}

func outputLines(output string) []string {
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines
}

func porcelainFiles(output string) []string {
	files := make([]string, 0)
	for _, line := range outputLines(output) {
		if len(line) < 4 {
			continue
		}
		files = append(files, line[3:])
	}
	return files
}

func sortedFiles(files map[string]struct{}) []string {
	result := make([]string, 0, len(files))
	for file := range files {
		result = append(result, file)
	}
	sort.Strings(result)
	return result
}
