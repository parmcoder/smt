package git

import (
	"context"
	"fmt"
	"strings"
)

// BranchSource identifies how a checkout branch was resolved.
type BranchSource string

const (
	// Local is a branch already present in the local repository.
	Local BranchSource = "local"
	// Remote is a branch that exists at origin and requires tracking setup.
	Remote BranchSource = "remote"
	// Default is a new branch created from the remote default or origin/main.
	Default BranchSource = "default"
)

// CheckoutStep is a fully resolved branch switch for one repository.
type CheckoutStep struct {
	Repository Repository
	Branch     string
	StartPoint string
	Source     BranchSource
	Create     bool
}

// PlanCheckout preflights and resolves every repository without switching it.
func PlanCheckout(ctx context.Context, runner Runner, repositories []Repository, branch string, dryRun bool) ([]CheckoutStep, error) {
	if runner == nil {
		return nil, fmt.Errorf("plan checkout: runner is required")
	}
	if branch == "" {
		return nil, fmt.Errorf("plan checkout: branch is required")
	}
	for _, repository := range repositories {
		state, err := Inspect(ctx, runner, repository)
		if err != nil {
			return nil, fmt.Errorf("preflight repository %s: %w", repository.ID, err)
		}
		// Detached and dirty repositories are rejected before any fetch or switch.
		if !state.Initialized {
			return nil, fmt.Errorf("preflight repository %s: repository is not initialized", repository.ID)
		}
		if state.Dirty {
			return nil, fmt.Errorf("preflight repository %s: repository is dirty", repository.ID)
		}
		if state.Detached {
			return nil, fmt.Errorf("preflight repository %s: repository is detached", repository.ID)
		}
	}

	if !dryRun {
		for _, repository := range repositories {
			result, err := runner.Run(ctx, repository.Dir, "fetch", "origin")
			if err != nil || hasNonZeroExit(result) {
				return nil, repositoryError("fetch", repository, commandError(result, err))
			}
		}
	}

	// Switches are deliberately deferred until every repository has a resolved step.
	steps := make([]CheckoutStep, 0, len(repositories))
	for _, repository := range repositories {
		step, err := resolveCheckout(ctx, runner, repository, branch)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// ExecuteCheckout performs the already-resolved switch operations in order.
func ExecuteCheckout(ctx context.Context, runner Runner, steps []CheckoutStep) error {
	if runner == nil {
		return fmt.Errorf("execute checkout: runner is required")
	}
	for _, step := range steps {
		args := []string{"switch"}
		if step.Create {
			if step.Source == Remote {
				args = append(args, "--track")
			}
			args = append(args, "--create", step.Branch, step.StartPoint)
		} else {
			args = append(args, step.Branch)
		}
		result, err := runner.Run(ctx, step.Repository.Dir, args...)
		if err != nil || hasNonZeroExit(result) {
			return repositoryError("switch", step.Repository, commandError(result, err))
		}
	}
	return nil
}

func resolveCheckout(ctx context.Context, runner Runner, repository Repository, branch string) (CheckoutStep, error) {
	local, err := refExists(ctx, runner, repository, "refs/heads/"+branch)
	if err != nil {
		return CheckoutStep{}, err
	}
	if local {
		return CheckoutStep{Repository: repository, Branch: branch, StartPoint: branch, Source: Local}, nil
	}

	remoteBranch := "origin/" + branch
	remote, err := refExists(ctx, runner, repository, "refs/remotes/"+remoteBranch)
	if err != nil {
		return CheckoutStep{}, err
	}
	if remote {
		return CheckoutStep{Repository: repository, Branch: branch, StartPoint: remoteBranch, Source: Remote, Create: true}, nil
	}

	defaultBranch, err := remoteDefaultBranch(ctx, runner, repository)
	if err != nil {
		return CheckoutStep{}, err
	}
	if defaultBranch != "" {
		return CheckoutStep{Repository: repository, Branch: branch, StartPoint: defaultBranch, Source: Default, Create: true}, nil
	}

	mainBranch := "origin/main"
	main, err := refExists(ctx, runner, repository, "refs/remotes/"+mainBranch)
	if err != nil {
		return CheckoutStep{}, err
	}
	if !main {
		return CheckoutStep{}, fmt.Errorf("resolve branch for repository %s at %s: origin/HEAD and origin/main are unavailable", repository.ID, repository.Dir)
	}
	return CheckoutStep{Repository: repository, Branch: branch, StartPoint: mainBranch, Source: Default, Create: true}, nil
}

func refExists(ctx context.Context, runner Runner, repository Repository, ref string) (bool, error) {
	result, err := runner.Run(ctx, repository.Dir, "show-ref", "--verify", "--quiet", ref)
	if hasNonZeroExit(result) {
		return false, nil
	}
	if err != nil {
		return false, repositoryError("resolve branch", repository, err)
	}
	return true, nil
}

func remoteDefaultBranch(ctx context.Context, runner Runner, repository Repository) (string, error) {
	result, err := runner.Run(ctx, repository.Dir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if hasNonZeroExit(result) {
		return "", nil
	}
	if err != nil {
		return "", repositoryError("resolve remote default", repository, err)
	}
	ref := strings.TrimSpace(result.Stdout)
	const prefix = "refs/remotes/"
	if ref == "" {
		return "", nil
	}
	if !strings.HasPrefix(ref, prefix+"origin/") {
		return "", fmt.Errorf("resolve remote default for repository %s at %s: unexpected reference %q", repository.ID, repository.Dir, ref)
	}
	return strings.TrimPrefix(ref, prefix), nil
}
