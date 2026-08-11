package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
)

type prepareBeadsService interface {
	ShowIssue(context.Context, string) (beads.Issue, error)
	ListOpenChildren(context.Context, string) ([]beads.Issue, error)
}

var newPrepareBeadsService = func(root string) prepareBeadsService {
	return beads.New(root, beads.CommandRunner{})
}

type prepareStepOutput struct {
	Repository string `json:"repository"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
}

type prepareOutput struct {
	Feature  string              `json:"feature"`
	Branch   string              `json:"branch"`
	DryRun   bool                `json:"dry_run"`
	Manifest string              `json:"manifest,omitempty"`
	Planned  []prepareStepOutput `json:"planned,omitempty"`
	Created  []prepareStepOutput `json:"created,omitempty"`
	Pending  []prepareStepOutput `json:"pending,omitempty"`
}

func runPrepare(ctx context.Context, cfg config.Config, root string, beadsService prepareBeadsService, runner git.Runner, featureID, destination, branch string, dryRun, jsonOutput bool, out, errOut io.Writer) int {
	if beadsService == nil || runner == nil {
		fmt.Fprintln(errOut, "workspace prepare: required services are unavailable")
		return exitInternal
	}
	feature, err := beadsService.ShowIssue(ctx, featureID)
	if err != nil {
		fmt.Fprintf(errOut, "workspace prepare: %v\n", err)
		return exitValidation
	}
	children, err := beadsService.ListOpenChildren(ctx, featureID)
	if err != nil {
		fmt.Fprintf(errOut, "workspace prepare: %v\n", err)
		return exitValidation
	}
	assignments, err := workspacepkg.ResolveAssignments(feature, cfg, children)
	if err != nil {
		fmt.Fprintf(errOut, "workspace prepare: %v\n", err)
		return exitValidation
	}

	targets := make([]git.WorktreeTarget, 0, len(cfg.Repositories))
	bases := make(map[string]workspacepkg.BaseState, len(cfg.Repositories))
	for _, repository := range cfg.Repositories {
		dir := root
		if filepath.Clean(repository.Path) != "." {
			dir = filepath.Join(root, repository.Path)
		}
		gitRepository := git.Repository{ID: repository.ID, Dir: dir, IsRoot: filepath.Clean(repository.Path) == "."}
		state, inspectErr := git.Inspect(ctx, runner, gitRepository)
		if inspectErr != nil {
			fmt.Fprintf(errOut, "workspace prepare: %v\n", inspectErr)
			return exitValidation
		}
		result, headErr := runner.Run(ctx, dir, "rev-parse", "HEAD")
		if headErr != nil || result.ExitCode != 0 || strings.TrimSpace(result.Stdout) == "" {
			fmt.Fprintf(errOut, "workspace prepare: repository %s has no readable HEAD\n", repository.ID)
			return exitValidation
		}
		bases[repository.ID] = workspacepkg.BaseState{Branch: state.Branch, Commit: strings.TrimSpace(result.Stdout)}
		targets = append(targets, git.WorktreeTarget{Repository: gitRepository, Path: repository.Path})
	}
	plan, err := git.PlanWorktree(ctx, runner, targets, destination, branch)
	if err != nil {
		fmt.Fprintf(errOut, "workspace prepare: %v\n", err)
		return exitValidation
	}
	output := prepareOutput{Feature: featureID, Branch: branch, DryRun: dryRun, Planned: prepareStepOutputs(plan.Steps)}
	if dryRun {
		return writePrepareOutput(output, jsonOutput, out, errOut)
	}
	report, err := git.ExecuteWorktree(ctx, runner, plan, false)
	output.Created = prepareStepOutputs(report.Created)
	output.Pending = prepareStepOutputs(report.Pending)
	if err != nil {
		if jsonOutput {
			_ = writePrepareOutput(output, jsonOutput, out, errOut)
		}
		fmt.Fprintf(errOut, "workspace prepare: %v\n", err)
		return exitValidation
	}
	manifest, err := workspacepkg.BuildRunManifest(assignments, cfg, destination, branch, bases)
	if err != nil {
		fmt.Fprintf(errOut, "workspace prepare: %v\n", err)
		return exitValidation
	}
	manifestPath, err := workspacepkg.WriteRunManifest(destination, manifest)
	if err != nil {
		fmt.Fprintf(errOut, "workspace prepare: worktrees created but manifest was not written: %v\n", err)
		return exitValidation
	}
	output.Manifest = manifestPath
	return writePrepareOutput(output, jsonOutput, out, errOut)
}

func prepareStepOutputs(steps []git.WorktreeStep) []prepareStepOutput {
	out := make([]prepareStepOutput, 0, len(steps))
	for _, step := range steps {
		out = append(out, prepareStepOutput{Repository: step.Repository.ID, Path: step.Destination, Branch: step.Branch})
	}
	return out
}

func writePrepareOutput(output prepareOutput, jsonOutput bool, out, errOut io.Writer) int {
	if output.DryRun && !jsonOutput {
		if len(output.Planned) == 0 {
			fmt.Fprintln(out, "workspace prepare plan: no repositories")
		} else {
			fmt.Fprintln(out, "workspace prepare plan:")
			for _, step := range output.Planned {
				fmt.Fprintf(out, "%s: %s\n", step.Repository, step.Path)
			}
		}
		return exitOK
	}
	if err := json.NewEncoder(out).Encode(output); err != nil {
		fmt.Fprintf(errOut, "workspace prepare: encode report: %v\n", err)
		return exitInternal
	}
	return exitOK
}

func preparedCommitReferences(ctx context.Context, configPath string, runner git.Runner) ([]string, bool, error) {
	currentPath, err := os.Getwd()
	if err != nil {
		return nil, false, errors.New("resolve current repository path")
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return nil, false, errors.New("resolve configuration path")
	}
	workspaceRoot := filepath.Dir(absConfig)
	runsDirectory := filepath.Join(workspaceRoot, ".smt", "runs")
	if _, err := os.Stat(runsDirectory); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, errors.New("inspect prepared workspace manifests")
	}
	state, err := git.Inspect(ctx, runner, git.Repository{ID: "current", Dir: currentPath})
	if err != nil || !state.Initialized || state.Detached || state.Branch == "" {
		return nil, true, errors.New("prepared workspace repository must be an attached Git worktree")
	}
	manifest, repository, err := workspacepkg.FindPreparedRepository(workspaceRoot, currentPath, state.Branch)
	if err != nil {
		return nil, true, err
	}
	allowed := make([]string, 0)
	if filepath.Clean(repository.Path) == "." {
		allowed = append(allowed, manifest.Feature.ID)
	}
	for _, task := range repository.Tasks {
		allowed = append(allowed, task.AllowedReferences...)
	}
	return allowed, true, nil
}
