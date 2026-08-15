package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
)

type BeadsLifecycle interface {
	CreatePreparedWorkspaceTask(context.Context) (string, error)
	ShowIssue(context.Context, string) (beads.Issue, error)
}

type RepositoryProgress struct{ ID, Status, Error string }
type LifecycleReport struct {
	TaskID  string
	Results []RepositoryProgress
	Pending []string
}

func repos(root string, cfg config.Config) []git.Repository {
	out := make([]git.Repository, 0, len(cfg.Repositories))
	for _, r := range cfg.Repositories {
		out = append(out, git.Repository{ID: r.ID, Dir: filepath.Join(root, r.Path), IsRoot: filepath.Clean(r.Path) == "."})
	}
	return out
}

func Prepare(ctx context.Context, cfg config.Config, root string, runner git.Runner, service BeadsLifecycle) (LifecycleReport, error) {
	report := LifecycleReport{}
	if runner == nil || service == nil {
		return report, errorsLifecycle("prepare: required services are unavailable")
	}
	r := repos(root, cfg)
	for i, repo := range r {
		if err := preflightRepo(ctx, runner, repo); err != nil {
			return report, fmt.Errorf("prepare: repository %s preflight failed", repo.ID)
		}
		if !branchExists(ctx, runner, repo, cfg.Repositories[i].EffectiveDefaultBranch()) {
			return report, fmt.Errorf("prepare: repository %s default branch is unavailable", repo.ID)
		}
	}
	id, err := service.CreatePreparedWorkspaceTask(ctx)
	if err != nil {
		return report, fmt.Errorf("prepare: create Beads task: %w", err)
	}
	report.TaskID = id
	for _, repo := range r {
		if branchExists(ctx, runner, repo, id) {
			return report, fmt.Errorf("prepare: target branch preflight failed for repository %s", repo.ID)
		}
	}
	for i, repo := range r {
		report.Results = append(report.Results, RepositoryProgress{ID: repo.ID, Status: "clean"})
		if err := runGit(ctx, runner, repo, "stash", "push", "--include-untracked", "--message", "smt prepared workspace "+id); err != nil {
			return failLifecycle(report, r[i:], repo, err)
		}
		report.Results = append(report.Results, RepositoryProgress{ID: repo.ID, Status: "stashed"})
		if err := runGit(ctx, runner, repo, "switch", cfg.Repositories[i].EffectiveDefaultBranch()); err != nil {
			return failLifecycle(report, r[i:], repo, err)
		}
		if err := runGit(ctx, runner, repo, "switch", "--create", id); err != nil {
			return failLifecycle(report, r[i:], repo, err)
		}
		report.Results = append(report.Results, RepositoryProgress{ID: repo.ID, Status: "created"})
	}
	return report, nil
}

func Switch(ctx context.Context, cfg config.Config, root, id string, runner git.Runner, service BeadsLifecycle) (LifecycleReport, error) {
	report := LifecycleReport{TaskID: id}
	if runner == nil || service == nil || strings.TrimSpace(id) == "" {
		return report, errorsLifecycle("switch: required services and Beads ID are required")
	}
	issue, err := service.ShowIssue(ctx, id)
	if err != nil || (issue.Status != "open" && issue.Status != "in_progress") {
		return report, errorsLifecycle("switch: Beads task is not active")
	}
	r := repos(root, cfg)
	for _, repo := range r {
		if err := preflightRepo(ctx, runner, repo); err != nil || !branchExists(ctx, runner, repo, id) {
			return report, fmt.Errorf("switch: branch preflight failed for repository %s", repo.ID)
		}
	}
	for i, repo := range r {
		report.Results = append(report.Results, RepositoryProgress{ID: repo.ID, Status: "clean"})
		if err := runGit(ctx, runner, repo, "stash", "push", "--include-untracked", "--message", "smt prepared workspace "+id); err != nil {
			return failLifecycle(report, r[i:], repo, err)
		}
		report.Results = append(report.Results, RepositoryProgress{ID: repo.ID, Status: "stashed"})
		if err := runGit(ctx, runner, repo, "switch", id); err != nil {
			return failLifecycle(report, r[i:], repo, err)
		}
		report.Results = append(report.Results, RepositoryProgress{ID: repo.ID, Status: "switched"})
	}
	return report, nil
}

func preflightRepo(ctx context.Context, r git.Runner, repo git.Repository) error {
	s, e := git.Inspect(ctx, r, repo)
	if e != nil || !s.Initialized || s.Detached {
		return errorsLifecycle("preflight")
	}
	return nil
}
func branchExists(ctx context.Context, r git.Runner, repo git.Repository, branch string) bool {
	if strings.TrimSpace(branch) == "" {
		return false
	}
	x, e := r.Run(ctx, repo.Dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return e == nil && x.ExitCode == 0
}
func runGit(ctx context.Context, r git.Runner, repo git.Repository, args ...string) error {
	x, e := r.Run(ctx, repo.Dir, args...)
	if e != nil || x.ExitCode != 0 {
		return fmt.Errorf("repository %s operation %s failed", repo.ID, args[0])
	}
	return nil
}
func failLifecycle(report LifecycleReport, pending []git.Repository, repo git.Repository, err error) (LifecycleReport, error) {
	report.Results = append(report.Results, RepositoryProgress{ID: repo.ID, Status: "pending", Error: err.Error()})
	for _, p := range pending {
		report.Pending = append(report.Pending, p.ID)
	}
	return report, err
}
func errorsLifecycle(message string) error { return fmt.Errorf("%s", message) }
