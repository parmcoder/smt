package git

import (
	"context"
	"fmt"
	"strings"
)

type PullTarget struct {
	Repository    Repository
	DefaultBranch string
	Branch        string
}
type PullPlan struct{ Targets []PullTarget }

func PlanPull(ctx context.Context, runner Runner, targets []PullTarget) (PullPlan, error) {
	if runner == nil || len(targets) == 0 {
		return PullPlan{}, fmt.Errorf("plan pull: required repositories and runner")
	}
	for index, target := range targets {
		state, err := Inspect(ctx, runner, target.Repository)
		if err != nil || !state.Initialized || state.Detached || state.Dirty {
			return PullPlan{}, fmt.Errorf("preflight repository %s failed", target.Repository.ID)
		}
		branch := strings.TrimSpace(state.Branch)
		if branch == "" {
			branch = strings.TrimSpace(target.DefaultBranch)
		}
		if branch == "" {
			branch = "main"
		}
		target.Branch = branch
		targets[index].Branch = branch
		result, err := runner.Run(ctx, target.Repository.Dir, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
		if err != nil || result.ExitCode != 0 {
			return PullPlan{}, fmt.Errorf("preflight repository %s remote branch unavailable", target.Repository.ID)
		}
	}
	ordered := append([]PullTarget(nil), targets...)
	for i := 0; i < len(ordered); i++ {
		if ordered[i].Repository.IsRoot {
			ordered = append(ordered[:i], append(ordered[i+1:], ordered[i])...)
			break
		}
	}
	return PullPlan{Targets: ordered}, nil
}
func ExecutePull(ctx context.Context, runner Runner, plan PullPlan) ([]PullTarget, error) {
	completed := []PullTarget{}
	for i, target := range plan.Targets {
		branch := target.Branch
		if branch == "" {
			branch = "main"
		}
		result, err := runner.Run(ctx, target.Repository.Dir, "pull", "--ff-only", "origin", branch)
		if err != nil || result.ExitCode != 0 {
			return completed, fmt.Errorf("pull repository %s failed", target.Repository.ID)
		}
		completed = append(completed, target)
		_ = i
	}
	return completed, nil
}
