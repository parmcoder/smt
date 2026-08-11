package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parmcoder/smt/internal/checks"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	providerpkg "github.com/parmcoder/smt/internal/provider"
	submissionpkg "github.com/parmcoder/smt/internal/submission"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
	"github.com/sirupsen/logrus"
)

type submitReviewOutput struct {
	Repository string `json:"repository"`
	Provider   string `json:"provider"`
	Status     string `json:"status"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Error      string `json:"error,omitempty"`
}

type submitOutput struct {
	Feature  string               `json:"feature"`
	Branch   string               `json:"branch"`
	DryRun   bool                 `json:"dry_run"`
	Planned  []submissionpkg.Step `json:"planned,omitempty"`
	Pushed   []string             `json:"pushed,omitempty"`
	Pending  []string             `json:"pending,omitempty"`
	Reviews  []submitReviewOutput `json:"reviews,omitempty"`
	Warnings []string             `json:"warnings,omitempty"`
}

func runWorkspaceSubmit(ctx context.Context, cfg config.Config, root, featureID string, runner git.Runner, ready, dryRun, jsonOutput bool, out, errOut io.Writer, logger *logrus.Logger) int {
	rootState, err := git.Inspect(ctx, runner, git.Repository{ID: "root", Dir: root, IsRoot: true})
	if err != nil || !rootState.Initialized || rootState.Detached || rootState.Branch == "" {
		fmt.Fprintln(errOut, "workspace submit: current workspace root is not an attached Git worktree")
		return exitValidation
	}
	manifest, err := workspacepkg.FindRunManifest(root, featureID, rootState.Branch)
	if err != nil {
		fmt.Fprintf(errOut, "workspace submit: %v\n", err)
		return exitValidation
	}
	var checkExecutor checks.Executor
	if logger != nil {
		checkExecutor = commandExecutor{logger: logger.WithField("profile", "submit")}
	}
	plan, err := submissionpkg.Plan(ctx, cfg, manifest, featureID, root, runner, checkExecutor, dryRun)
	if err != nil {
		fmt.Fprintf(errOut, "%v\n", err)
		return exitValidation
	}
	output := submitOutput{Feature: featureID, Branch: manifest.Branch, DryRun: dryRun, Planned: plan.Steps}
	if dryRun {
		return writeSubmitOutput(output, jsonOutput, out, errOut)
	}
	pushPlan := git.PushPlan{Steps: make([]git.PushStep, 0, len(plan.Steps))}
	for _, step := range plan.Steps {
		pushPlan.Steps = append(pushPlan.Steps, git.PushStep{Repository: git.Repository{ID: step.ID, Dir: filepath.Join(root, step.Path), IsRoot: filepath.Clean(step.Path) == "."}, Branch: step.Branch, RemoteURL: step.RemoteURL})
	}
	pushed, pushErr := git.ExecutePush(ctx, runner, pushPlan, false)
	for _, step := range pushed.Pushed {
		output.Pushed = append(output.Pushed, step.Repository.ID)
	}
	for _, step := range pushed.Pending {
		output.Pending = append(output.Pending, step.Repository.ID)
	}
	pushedIDs := make(map[string]struct{}, len(pushed.Pushed))
	for _, step := range pushed.Pushed {
		pushedIDs[step.Repository.ID] = struct{}{}
	}
	childLinks := make(map[string]string)
	reviewProviders := make(map[string]providerpkg.ReviewProvider)
	providerErrors := false
	for _, step := range plan.Steps {
		if _, ok := pushedIDs[step.ID]; !ok {
			continue
		}
		repository, ok := configuredRepository(cfg, step.ID)
		if !ok {
			providerErrors = true
			output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Status: "error", Error: "repository is not configured"})
			continue
		}
		entry, ok := manifestRepository(manifest, step.ID)
		if !ok {
			providerErrors = true
			output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Status: "error", Error: "repository is not in the prepared manifest"})
			continue
		}
		title, body, contentErr := submissionpkg.ReviewContent(manifest, step.ID, childLinks)
		if contentErr != nil {
			providerErrors = true
			output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Status: "error", Error: contentErr.Error()})
			continue
		}
		settings := providerSettings(cfg, repository.Provider)
		if filepath.Clean(step.Path) == "." && !allChildLinks(plan, childLinks) {
			output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Provider: repository.Provider, Status: "deferred", Title: title, Body: body, Error: "child review URLs are not available"})
			output.Warnings = append(output.Warnings, "root review deferred until child review URLs are available")
			continue
		}
		token := os.Getenv("SMT_" + strings.ToUpper(repository.Provider) + "_TOKEN")
		if strings.TrimSpace(token) == "" {
			link := submissionpkg.ReviewLink(repository.Provider, settings, repository.Project, manifest.Branch, entry.BaseBranch)
			output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Provider: repository.Provider, Status: "handoff", URL: link, Title: title, Body: body})
			output.Warnings = append(output.Warnings, repository.ID+" review requires human provider handoff")
			continue
		}
		reviewProvider, providerErr := reviewProviders[repository.Provider], error(nil)
		if reviewProvider == nil {
			_, reviewProvider, providerErr = providerpkg.NewConfigured(repository.Provider, settings, token, nil)
			if providerErr == nil {
				reviewProviders[repository.Provider] = reviewProvider
			}
		}
		if providerErr != nil {
			providerErrors = true
			output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Provider: repository.Provider, Status: "error", Title: title, Body: body, Error: safeProviderError(providerErr)})
			continue
		}
		spec := providerpkg.ReviewSpec{Project: repository.Project, SourceBranch: manifest.Branch, TargetBranch: entry.BaseBranch, Title: title, Description: body, Draft: !ready}
		reviews, findErr := reviewProvider.FindOpenReviews(ctx, spec)
		if findErr != nil {
			providerErrors = true
			output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Provider: repository.Provider, Status: "error", Title: title, Body: body, Error: safeProviderError(findErr)})
			continue
		}
		var review providerpkg.ReviewInfo
		status := "created"
		if len(reviews) > 0 {
			sort.SliceStable(reviews, func(i, j int) bool { return reviews[i].ID < reviews[j].ID })
			review = reviews[0]
			status = "reused"
			if ready && review.Draft {
				review, findErr = reviewProvider.SetReady(ctx, review)
				if findErr != nil {
					providerErrors = true
					output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Provider: repository.Provider, Status: "error", Title: title, Body: body, Error: safeProviderError(findErr)})
					continue
				}
			}
		} else {
			review, findErr = reviewProvider.CreateReview(ctx, spec)
			if findErr != nil {
				providerErrors = true
				output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Provider: repository.Provider, Status: "error", Title: title, Body: body, Error: safeProviderError(findErr)})
				continue
			}
		}
		output.Reviews = append(output.Reviews, submitReviewOutput{Repository: step.ID, Provider: repository.Provider, Status: status, URL: review.URL, Title: title, Body: body})
		if filepath.Clean(step.Path) != "." && review.URL != "" {
			childLinks[step.ID] = review.URL
		}
	}
	if code := writeSubmitOutput(output, jsonOutput, out, errOut); code != exitOK {
		return code
	}
	if pushErr != nil || providerErrors {
		if pushErr != nil {
			fmt.Fprintf(errOut, "workspace submit: %v\n", pushErr)
		}
		return exitValidation
	}
	return exitOK
}

func writeSubmitOutput(output submitOutput, jsonOutput bool, out, errOut io.Writer) int {
	if jsonOutput {
		if err := json.NewEncoder(out).Encode(output); err != nil {
			fmt.Fprintf(errOut, "workspace submit: encode report: %v\n", err)
			return exitInternal
		}
		return exitOK
	}
	if output.DryRun {
		fmt.Fprintln(out, "workspace submit plan:")
	} else {
		fmt.Fprintln(out, "workspace submit:")
	}
	for _, step := range output.Planned {
		fmt.Fprintf(out, "- %s: %d commit(s)\n", step.ID, step.CommitCount)
	}
	if len(output.Pushed) > 0 {
		fmt.Fprintf(out, "pushed: %s\n", strings.Join(output.Pushed, ", "))
	}
	if len(output.Pending) > 0 {
		fmt.Fprintf(out, "pending: %s\n", strings.Join(output.Pending, ", "))
	}
	for _, review := range output.Reviews {
		fmt.Fprintf(out, "review %s: %s", review.Repository, review.Status)
		if review.URL != "" {
			fmt.Fprintf(out, " %s", review.URL)
		}
		fmt.Fprintln(out)
		if review.Status == "handoff" || review.Status == "deferred" {
			fmt.Fprintf(out, "title: %s\nbody:\n%s", review.Title, review.Body)
		}
		if review.Error != "" {
			fmt.Fprintf(out, "reason: %s\n", review.Error)
		}
	}
	for _, warning := range output.Warnings {
		fmt.Fprintf(out, "warning: %s\n", warning)
	}
	return exitOK
}

func configuredRepository(cfg config.Config, id string) (config.Repository, bool) {
	for _, repository := range cfg.Repositories {
		if repository.ID == id {
			return repository, true
		}
	}
	return config.Repository{}, false
}

func manifestRepository(manifest workspacepkg.RunManifest, id string) (workspacepkg.ManifestRepository, bool) {
	for _, repository := range manifest.Repositories {
		if repository.ID == id {
			return repository, true
		}
	}
	return workspacepkg.ManifestRepository{}, false
}

func providerSettings(cfg config.Config, name string) config.ProviderConfig {
	if name == "gitlab" {
		return cfg.Providers.GitLab
	}
	return cfg.Providers.GitHub
}

func allChildLinks(plan submissionpkg.SubmissionPlan, links map[string]string) bool {
	for _, step := range plan.Steps {
		if filepath.Clean(step.Path) != "." {
			if _, ok := links[step.ID]; !ok {
				return false
			}
		}
	}
	return true
}

func safeProviderError(err error) string {
	if err == nil {
		return ""
	}
	if strings.Contains(strings.ToLower(err.Error()), "token") {
		return "provider request failed"
	}
	return err.Error()
}
