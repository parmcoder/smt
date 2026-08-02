package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	ggit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

type PushTarget struct {
	Repository          Repository
	RemoteURL, Provider string
}
type PushStep struct {
	Repository                  Repository
	Branch, RemoteURL, Provider string
}
type PushPlan struct{ Steps []PushStep }
type PushReport struct {
	Planned, Pushed, Pending []PushStep
	DryRun                   bool
}

func PlanPush(ctx context.Context, targets []PushTarget) (PushPlan, error) {
	if len(targets) == 0 {
		return PushPlan{}, fmt.Errorf("plan push: at least one repository is required")
	}
	children, roots := []PushStep{}, []PushStep{}
	for _, target := range targets {
		if strings.TrimSpace(target.RemoteURL) == "" {
			return PushPlan{}, fmt.Errorf("preflight repository %s: remote.url is required", target.Repository.ID)
		}
		state, err := Inspect(ctx, target.Repository)
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
		step := PushStep{Repository: target.Repository, Branch: state.Branch, RemoteURL: target.RemoteURL, Provider: target.Provider}
		if target.Repository.IsRoot {
			roots = append(roots, step)
		} else {
			children = append(children, step)
		}
	}
	return PushPlan{Steps: append(children, roots...)}, nil
}
func ExecutePush(ctx context.Context, plan PushPlan, dryRun bool) (PushReport, error) {
	report := PushReport{Planned: append([]PushStep(nil), plan.Steps...), DryRun: dryRun}
	if dryRun {
		return report, nil
	}
	for index, step := range plan.Steps {
		if err := pushStep(ctx, step); err != nil {
			report.Pending = append(report.Pending, plan.Steps[index+1:]...)
			return report, fmt.Errorf("push repository %s branch %s failed", step.Repository.ID, step.Branch)
		}
		report.Pushed = append(report.Pushed, step)
	}
	return report, nil
}

var pushStep = push

func push(ctx context.Context, step PushStep) error {
	r, err := Open(step.Repository.Dir)
	if err != nil {
		return err
	}
	auth, err := authFor(step)
	if err != nil {
		return err
	}
	remote := ggit.NewRemote(r.Storer, &gitconfig.RemoteConfig{Name: "smt", URLs: []string{step.RemoteURL}})
	err = remote.PushContext(ctx, &ggit.PushOptions{RemoteName: "smt", RemoteURL: step.RemoteURL, RefSpecs: []gitconfig.RefSpec{gitconfig.RefSpec("refs/heads/" + step.Branch + ":refs/heads/" + step.Branch)}, Auth: auth})
	if err == ggit.NoErrAlreadyUpToDate {
		return nil
	}
	return err
}
func authFor(step PushStep) (transport.AuthMethod, error) {
	ep, err := transport.NewEndpoint(step.RemoteURL)
	if err != nil {
		return nil, fmt.Errorf("configure push authentication: invalid remote URL")
	}
	switch ep.Protocol {
	case "http", "https":
		token := ""
		if step.Provider == "github" || strings.EqualFold(ep.Host, "github.com") {
			token = os.Getenv("SMT_GITHUB_TOKEN")
		} else if step.Provider == "gitlab" || strings.EqualFold(ep.Host, "gitlab.com") {
			token = os.Getenv("SMT_GITLAB_TOKEN")
		}
		if token == "" {
			return nil, nil
		}
		return &githttp.BasicAuth{Username: "git", Password: token}, nil
	case "ssh":
		auth, err := gitssh.NewSSHAgentAuth(ep.User)
		if err != nil {
			return nil, fmt.Errorf("configure SSH agent authentication: %w", err)
		}
		callback, err := gitssh.NewKnownHostsCallback()
		if err != nil {
			return nil, fmt.Errorf("configure SSH known_hosts verification: %w", err)
		}
		auth.HostKeyCallback = callback
		return auth, nil
	}
	return nil, nil
}
