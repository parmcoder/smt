// Package submission plans safe, traceable workspace branch submissions.
package submission

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/parmcoder/smt/internal/checks"
	"github.com/parmcoder/smt/internal/commit"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
)

// Step is one repository branch selected for submission. RemoteURL and
// Commits are execution-only fields and are intentionally omitted from JSON.
type Step struct {
	ID          string              `json:"id"`
	Path        string              `json:"path"`
	Branch      string              `json:"branch"`
	CommitCount int                 `json:"commit_count"`
	RemoteURL   string              `json:"-"`
	Commits     []git.CommitMessage `json:"-"`
}

// SubmissionPlan is deterministic and contains only selected repositories.
type SubmissionPlan struct {
	Feature string `json:"feature"`
	Branch  string `json:"branch"`
	Steps   []Step `json:"steps"`
}

// Plan builds a push plan without pushing or calling a provider API.
func Plan(ctx context.Context, cfg config.Config, manifest workspacepkg.RunManifest, featureID, workspacePath string, runner git.Runner, checkExecutor checks.Executor, dryRun bool) (SubmissionPlan, error) {
	result := SubmissionPlan{Feature: featureID, Branch: manifest.Branch, Steps: []Step{}}
	if runner == nil {
		return result, errors.New("workspace submit: runner is required")
	}
	root, err := filepath.Abs(workspacePath)
	if err != nil || filepath.Clean(manifest.WorkspacePath) != root {
		return result, errors.New("workspace submit: prepared workspace path does not match")
	}
	if manifest.SchemaVersion != 1 || featureID == "" || manifest.Feature.ID != featureID || manifest.Branch == "" {
		return result, errors.New("workspace submit: prepared workspace manifest does not match feature")
	}
	byID := make(map[string]workspacepkg.ManifestRepository, len(manifest.Repositories))
	for _, repository := range manifest.Repositories {
		if repository.ID == "" || repository.Path == "" {
			return result, errors.New("workspace submit: prepared manifest contains an invalid repository")
		}
		if _, exists := byID[repository.ID]; exists {
			return result, errors.New("workspace submit: prepared manifest contains duplicate repositories")
		}
		byID[repository.ID] = repository
	}
	if len(byID) != len(cfg.Repositories) {
		return result, errors.New("workspace submit: prepared manifest repository set does not match configuration")
	}
	selected := make(map[string]Step)
	for _, repository := range cfg.Repositories {
		entry, ok := byID[repository.ID]
		if !ok || filepath.Clean(entry.Path) != filepath.Clean(repository.Path) {
			return result, fmt.Errorf("workspace submit: prepared manifest does not match repository %s", repository.ID)
		}
		directory := filepath.Join(root, repository.Path)
		state, inspectErr := git.Inspect(ctx, runner, git.Repository{ID: repository.ID, Dir: directory, IsRoot: filepath.Clean(repository.Path) == "."})
		if inspectErr != nil {
			return result, fmt.Errorf("workspace submit: %w", inspectErr)
		}
		if !state.Initialized || state.Dirty || state.Detached || state.Branch != manifest.Branch {
			return result, fmt.Errorf("workspace submit: repository %s is not a clean attached worktree on the prepared branch", repository.ID)
		}
		if strings.TrimSpace(repository.Remote.URL) == "" {
			return result, fmt.Errorf("workspace submit: repository %s has no configured remote", repository.ID)
		}
		origin, originErr := originURL(ctx, runner, directory)
		if originErr != nil || origin != repository.Remote.URL {
			return result, fmt.Errorf("workspace submit: repository %s origin does not match configured remote", repository.ID)
		}
		count, countErr := commitsAhead(ctx, runner, directory, entry.BaseCommit)
		if countErr != nil {
			return result, fmt.Errorf("workspace submit: repository %s has no readable prepared base", repository.ID)
		}
		if count == 0 {
			continue
		}
		if err := remoteBranchExists(ctx, runner, directory, entry.BaseBranch); err != nil {
			return result, fmt.Errorf("workspace submit: repository %s target branch is unavailable", repository.ID)
		}
		messages, messageErr := git.CommitMessages(ctx, runner, git.Repository{ID: repository.ID, Dir: directory}, entry.BaseCommit, "HEAD")
		if messageErr != nil {
			return result, fmt.Errorf("workspace submit: read repository %s commits", repository.ID)
		}
		allowed := allowedReferences(manifest, entry, filepath.Clean(repository.Path) == ".")
		policy := commit.Policy{Types: cfg.Commit.Types, Scopes: cfg.Commit.Scopes}
		for _, message := range messages {
			if err := commit.ValidatePreparedMessage(message.Message, policy, allowed); err != nil {
				return result, fmt.Errorf("workspace submit: repository %s has an invalid assigned commit", repository.ID)
			}
		}
		changedFiles, filesErr := changedFilesBetween(ctx, runner, directory, entry.BaseCommit)
		if filesErr != nil {
			return result, fmt.Errorf("workspace submit: inspect repository %s changes", repository.ID)
		}
		if checksExist(repository, "submit") {
			if err := checks.RunProfile(ctx, checkExecutor, repositoryWithDirectory(repository, directory), "submit", changedFiles, false, dryRun); err != nil {
				return result, fmt.Errorf("workspace submit: repository %s submit checks failed", repository.ID)
			}
		}
		selected[repository.ID] = Step{ID: repository.ID, Path: repository.Path, Branch: manifest.Branch, CommitCount: count, RemoteURL: repository.Remote.URL, Commits: messages}
	}

	_, rootStep, hasRoot := findRoot(cfg, selected)
	rootDirectory := ""
	rootBaseCommit := ""
	for _, repository := range cfg.Repositories {
		if filepath.Clean(repository.Path) == "." {
			rootDirectory = filepath.Join(root, repository.Path)
			rootBaseCommit = byID[repository.ID].BaseCommit
			break
		}
	}
	for _, repository := range cfg.Repositories {
		if filepath.Clean(repository.Path) == "." {
			continue
		}
		if _, childSelected := selected[repository.ID]; !childSelected {
			continue
		}
		if !hasRoot {
			return result, fmt.Errorf("workspace submit: changed child %s has no root integration commit", repository.ID)
		}
		changed, err := pathChanged(ctx, runner, rootDirectory, rootBaseCommit, repository.Path)
		if err != nil || !changed {
			return result, fmt.Errorf("workspace submit: root gitlink for child %s is not integrated", repository.ID)
		}
	}
	children := make([]Step, 0, len(selected))
	for _, repository := range cfg.Repositories {
		if filepath.Clean(repository.Path) == "." {
			continue
		}
		if step, ok := selected[repository.ID]; ok {
			children = append(children, step)
		}
	}
	result.Steps = append(children, result.Steps...)
	if hasRoot {
		result.Steps = append(result.Steps, rootStep)
	}
	if !hasRoot && len(result.Steps) == 0 {
		return result, errors.New("workspace submit: no assigned commits are ahead of the prepared base")
	}
	return result, nil
}

func allowedReferences(manifest workspacepkg.RunManifest, entry workspacepkg.ManifestRepository, root bool) []string {
	allowed := make([]string, 0)
	if root {
		allowed = append(allowed, manifest.Feature.ID)
	}
	for _, task := range entry.Tasks {
		allowed = append(allowed, task.AllowedReferences...)
	}
	return allowed
}

func findRoot(cfg config.Config, selected map[string]Step) (string, Step, bool) {
	for _, repository := range cfg.Repositories {
		if filepath.Clean(repository.Path) == "." {
			step, ok := selected[repository.ID]
			return repository.ID, step, ok
		}
	}
	return "", Step{}, false
}

func repositoryWithDirectory(repository config.Repository, directory string) config.Repository {
	repository.Path = directory
	return repository
}

func checksExist(repository config.Repository, profile string) bool {
	_, ok := repository.Profiles[profile]
	return ok
}

func originURL(ctx context.Context, runner git.Runner, directory string) (string, error) {
	result, err := runner.Run(ctx, directory, "remote", "get-url", "origin")
	if err != nil || result.ExitCode != 0 {
		return "", errors.New("origin is unavailable")
	}
	return strings.TrimSpace(result.Stdout), nil
}

func remoteBranchExists(ctx context.Context, runner git.Runner, directory, branch string) error {
	result, err := runner.Run(ctx, directory, "ls-remote", "--exit-code", "origin", "refs/heads/"+branch)
	if err != nil || result.ExitCode != 0 {
		return errors.New("target branch is unavailable")
	}
	return nil
}

func commitsAhead(ctx context.Context, runner git.Runner, directory, base string) (int, error) {
	result, err := runner.Run(ctx, directory, "rev-list", "--count", base+"..HEAD")
	if err != nil || result.ExitCode != 0 {
		return 0, errors.New("prepared base is unavailable")
	}
	count, err := strconv.Atoi(strings.TrimSpace(result.Stdout))
	if err != nil || count < 0 {
		return 0, errors.New("prepared base count is invalid")
	}
	return count, nil
}

func changedFilesBetween(ctx context.Context, runner git.Runner, directory, base string) ([]string, error) {
	result, err := runner.Run(ctx, directory, "diff", "--name-only", base+"..HEAD", "--")
	if err != nil || result.ExitCode != 0 {
		return nil, errors.New("changed files are unavailable")
	}
	return splitLines(result.Stdout), nil
}

func pathChanged(ctx context.Context, runner git.Runner, directory, base, path string) (bool, error) {
	result, err := runner.Run(ctx, directory, "diff", "--name-only", base+"..HEAD", "--", path)
	if err != nil || result.ExitCode != 0 {
		return false, errors.New("root integration changes are unavailable")
	}
	for _, line := range splitLines(result.Stdout) {
		if line == path {
			return true, nil
		}
	}
	return false, nil
}

func splitLines(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines
}
