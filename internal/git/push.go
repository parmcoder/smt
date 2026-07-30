package git

import (
	"context"
	"fmt"
	"strings"
)

// PushTarget associates a configured repository with its credential-free
// destination URL.
type PushTarget struct {
	Repository Repository
	RemoteURL  string
}

// PushStep is one fully preflighted current-branch push.
type PushStep struct {
	Repository Repository
	Branch     string
	RemoteURL  string
}

// PushPlan contains child-repository pushes followed by the root repository.
type PushPlan struct {
	Steps []PushStep
}

// PushReport identifies planned, pushed, and pending repositories without
// exposing configured remote URLs in human-facing errors.
type PushReport struct {
	Planned []PushStep
	Pushed  []PushStep
	Pending []PushStep
	DryRun  bool
}

// PlanPush validates every repository before constructing any remote action.
func PlanPush(ctx context.Context, runner Runner, targets []PushTarget) (PushPlan, error) {
	if runner == nil {
		return PushPlan{}, fmt.Errorf("plan push: runner is required")
	}
	if len(targets) == 0 {
		return PushPlan{}, fmt.Errorf("plan push: at least one repository is required")
	}
	children := make([]PushStep, 0, len(targets))
	roots := make([]PushStep, 0, 1)
	for _, target := range targets {
		if strings.TrimSpace(target.RemoteURL) == "" {
			return PushPlan{}, fmt.Errorf("preflight repository %s: remote.url is required", target.Repository.ID)
		}
		state, err := Inspect(ctx, runner, target.Repository)
		if err != nil {
			return PushPlan{}, fmt.Errorf("preflight repository %s: %w", target.Repository.ID, err)
		}
		if !state.Initialized {
			return PushPlan{}, fmt.Errorf("preflight repository %s: repository is not initialized", target.Repository.ID)
		}
		if state.Dirty {
			return PushPlan{}, fmt.Errorf("preflight repository %s: repository is dirty", target.Repository.ID)
		}
		if state.Detached || state.Branch == "" {
			return PushPlan{}, fmt.Errorf("preflight repository %s: repository is detached", target.Repository.ID)
		}
		step := PushStep{Repository: target.Repository, Branch: state.Branch, RemoteURL: target.RemoteURL}
		if target.Repository.IsRoot {
			roots = append(roots, step)
			continue
		}
		children = append(children, step)
	}
	return PushPlan{Steps: append(children, roots...)}, nil
}

// ExecutePush pushes a preflighted plan without force, commit, or rollback.
func ExecutePush(ctx context.Context, runner Runner, plan PushPlan, dryRun bool) (PushReport, error) {
	report := PushReport{Planned: append([]PushStep(nil), plan.Steps...), DryRun: dryRun}
	if runner == nil {
		return report, fmt.Errorf("execute push: runner is required")
	}
	if dryRun {
		return report, nil
	}
	for index, step := range plan.Steps {
		result, err := runner.Run(ctx, step.Repository.Dir, "push", step.RemoteURL, "HEAD:refs/heads/"+step.Branch)
		if err != nil || hasNonZeroExit(result) {
			report.Pending = append(report.Pending, plan.Steps[index+1:]...)
			return report, fmt.Errorf("push repository %s branch %s failed", step.Repository.ID, step.Branch)
		}
		report.Pushed = append(report.Pushed, step)
	}
	return report, nil
}
