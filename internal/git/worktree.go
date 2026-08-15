package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeTarget identifies one configured root or submodule and its path
// relative to the root worktree destination.
type WorktreeTarget struct {
	Repository    Repository
	Path          string
	DefaultBranch string
}

// WorktreeStep is one linked-worktree creation after all preflight succeeds.
type WorktreeStep struct {
	Repository  Repository
	Destination string
	Branch      string
	StartPoint  string
}

// WorktreePlan contains a root step followed by nested child worktrees.
type WorktreePlan struct {
	Steps []WorktreeStep
}

// WorktreeReport identifies planned, created, and pending worktrees.
type WorktreeReport struct {
	Planned []WorktreeStep
	Created []WorktreeStep
	Pending []WorktreeStep
	DryRun  bool
}

// PlanWorktree validates every source repository and creates a deterministic
// root-plus-child worktree plan without changing Git state.
func PlanWorktree(ctx context.Context, runner Runner, targets []WorktreeTarget, destination, branch string) (WorktreePlan, error) {
	if runner == nil {
		return WorktreePlan{}, fmt.Errorf("plan worktree: runner is required")
	}
	if len(targets) == 0 {
		return WorktreePlan{}, fmt.Errorf("plan worktree: at least one repository is required")
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return WorktreePlan{}, fmt.Errorf("plan worktree: resolve destination: %w", err)
	}
	if err := validateWorktreeDestination(destination, targets); err != nil {
		return WorktreePlan{}, err
	}
	root, children, err := splitWorktreeTargets(targets)
	if err != nil {
		return WorktreePlan{}, err
	}
	if err := validateBranch(ctx, runner, root.Repository, branch); err != nil {
		return WorktreePlan{}, err
	}

	for _, target := range targets {
		state, err := Inspect(ctx, runner, target.Repository)
		if err != nil {
			return WorktreePlan{}, fmt.Errorf("preflight repository %s: %w", target.Repository.ID, err)
		}
		if !state.Initialized {
			return WorktreePlan{}, fmt.Errorf("preflight repository %s: repository is not initialized", target.Repository.ID)
		}
		if state.Dirty {
			return WorktreePlan{}, fmt.Errorf("preflight repository %s: repository is dirty", target.Repository.ID)
		}
		if state.Detached || state.Branch == "" {
			return WorktreePlan{}, fmt.Errorf("preflight repository %s: repository is detached", target.Repository.ID)
		}
	}
	if err := ensureNewBranch(ctx, runner, targets, branch); err != nil {
		return WorktreePlan{}, err
	}
	heads, err := repositoryHeads(ctx, runner, targets)
	if err != nil {
		return WorktreePlan{}, err
	}
	if err := verifyGitlinks(ctx, runner, root, children, heads, root.DefaultBranch); err != nil {
		return WorktreePlan{}, err
	}

	steps := make([]WorktreeStep, 0, len(targets))
	start := root.DefaultBranch
	if start == "" {
		start = "main"
	}
	steps = append(steps, WorktreeStep{Repository: root.Repository, Destination: destination, Branch: branch, StartPoint: start})
	for _, child := range children {
		steps = append(steps, WorktreeStep{
			Repository: child.Repository, Destination: filepath.Join(destination, child.Path), Branch: branch, StartPoint: func() string {
				if child.DefaultBranch != "" {
					return child.DefaultBranch
				}
				return "main"
			}(),
		})
	}
	return WorktreePlan{Steps: steps}, nil
}

// ExecuteWorktree creates the linked worktrees in planned order. It does not
// remove already-created worktrees when a later child creation fails.
func ExecuteWorktree(ctx context.Context, runner Runner, plan WorktreePlan, dryRun bool) (WorktreeReport, error) {
	report := WorktreeReport{Planned: append([]WorktreeStep(nil), plan.Steps...), DryRun: dryRun}
	if runner == nil {
		return report, fmt.Errorf("execute worktree: runner is required")
	}
	if dryRun {
		return report, nil
	}
	for index, step := range plan.Steps {
		result, err := runner.Run(ctx, step.Repository.Dir, "worktree", "add", "-b", step.Branch, step.Destination, step.StartPoint)
		if err != nil || hasNonZeroExit(result) {
			report.Pending = append(report.Pending, plan.Steps[index+1:]...)
			return report, fmt.Errorf("create worktree for repository %s branch %s failed", step.Repository.ID, step.Branch)
		}
		report.Created = append(report.Created, step)
	}
	return report, nil
}

func validateWorktreeDestination(destination string, targets []WorktreeTarget) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("plan worktree: destination %s already exists", destination)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("plan worktree: inspect destination: %w", err)
	}
	for _, target := range targets {
		source, err := filepath.Abs(target.Repository.Dir)
		if err != nil {
			return fmt.Errorf("plan worktree: resolve repository %s: %w", target.Repository.ID, err)
		}
		if pathContains(source, destination) || pathContains(destination, source) {
			return fmt.Errorf("plan worktree: destination must be outside repository %s", target.Repository.ID)
		}
		if target.Path == "" || filepath.IsAbs(target.Path) || target.Path == ".." || strings.HasPrefix(filepath.Clean(target.Path), ".."+string(filepath.Separator)) {
			return fmt.Errorf("plan worktree: repository %s path must be workspace-relative", target.Repository.ID)
		}
	}
	return nil
}

func splitWorktreeTargets(targets []WorktreeTarget) (WorktreeTarget, []WorktreeTarget, error) {
	var root WorktreeTarget
	children := make([]WorktreeTarget, 0, len(targets)-1)
	for _, target := range targets {
		if target.Repository.IsRoot {
			if root.Repository.ID != "" {
				return WorktreeTarget{}, nil, fmt.Errorf("plan worktree: exactly one root repository is required")
			}
			if filepath.Clean(target.Path) != "." {
				return WorktreeTarget{}, nil, fmt.Errorf("plan worktree: root repository path must be .")
			}
			root = target
			continue
		}
		children = append(children, target)
	}
	if root.Repository.ID == "" {
		return WorktreeTarget{}, nil, fmt.Errorf("plan worktree: exactly one root repository is required")
	}
	return root, children, nil
}

func validateBranch(ctx context.Context, runner Runner, repository Repository, branch string) error {
	if strings.TrimSpace(branch) == "" {
		return fmt.Errorf("plan worktree: branch is required")
	}
	result, err := runner.Run(ctx, repository.Dir, "check-ref-format", "--branch", branch)
	if err != nil || hasNonZeroExit(result) {
		return fmt.Errorf("plan worktree: branch %q is invalid", branch)
	}
	return nil
}

func ensureNewBranch(ctx context.Context, runner Runner, targets []WorktreeTarget, branch string) error {
	for _, target := range targets {
		result, err := runner.Run(ctx, target.Repository.Dir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
		if hasNonZeroExit(result) {
			continue
		}
		if err != nil {
			return fmt.Errorf("preflight repository %s: inspect branch: %w", target.Repository.ID, err)
		}
		return fmt.Errorf("preflight repository %s: branch %q already exists or is checked out", target.Repository.ID, branch)
	}
	return nil
}

func repositoryHeads(ctx context.Context, runner Runner, targets []WorktreeTarget) (map[string]string, error) {
	heads := make(map[string]string, len(targets))
	for _, target := range targets {
		branch := target.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		result, err := runner.Run(ctx, target.Repository.Dir, "rev-parse", branch)
		if err != nil || hasNonZeroExit(result) {
			return nil, fmt.Errorf("preflight repository %s: read HEAD", target.Repository.ID)
		}
		head := strings.TrimSpace(result.Stdout)
		if head == "" {
			return nil, fmt.Errorf("preflight repository %s: HEAD is empty", target.Repository.ID)
		}
		heads[target.Repository.ID] = head
	}
	return heads, nil
}

func verifyGitlinks(ctx context.Context, runner Runner, root WorktreeTarget, children []WorktreeTarget, heads map[string]string, rootBranch string) error {
	if rootBranch == "" {
		rootBranch = "main"
	}
	for _, child := range children {
		result, err := runner.Run(ctx, root.Repository.Dir, "ls-tree", rootBranch, "--", child.Path)
		if err != nil || hasNonZeroExit(result) {
			return fmt.Errorf("preflight repository %s: read root gitlink", child.Repository.ID)
		}
		fields := strings.Fields(strings.TrimSpace(result.Stdout))
		if len(fields) < 3 || fields[0] != "160000" || fields[1] != "commit" {
			return fmt.Errorf("preflight repository %s: root gitlink is missing", child.Repository.ID)
		}
		if fields[2] != heads[child.Repository.ID] {
			return fmt.Errorf("preflight repository %s: root gitlink does not match child HEAD", child.Repository.ID)
		}
	}
	return nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
