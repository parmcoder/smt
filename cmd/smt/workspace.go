package main

import (
	"context"
	"fmt"
	"io"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
)

type lifecycleBeadsService interface{ workspacepkg.BeadsLifecycle }

var newLifecycleBeadsService = func(root string) lifecycleBeadsService { return beads.New(root, beads.CommandRunner{}) }

var newVerboseLifecycleBeadsService = func(root string, verbose io.Writer) lifecycleBeadsService {
	return beads.NewWithVerbose(root, beads.CommandRunner{}, verbose)
}

func runRepositoryPrepare(ctx context.Context, cfg config.Config, root string, service lifecycleBeadsService, runner git.Runner, out, errOut io.Writer) int {
	report, err := workspacepkg.Prepare(ctx, cfg, root, runner, service)
	writeLifecycleReport("prepare", report, out, errOut)
	if err != nil {
		return exitValidation
	}
	return exitOK
}
func runRepositorySwitch(ctx context.Context, cfg config.Config, root, id string, service lifecycleBeadsService, runner git.Runner, out, errOut io.Writer) int {
	report, err := workspacepkg.Switch(ctx, cfg, root, id, runner, service)
	writeLifecycleReport("switch", report, out, errOut)
	if err != nil {
		return exitValidation
	}
	return exitOK
}
func writeLifecycleReport(operation string, report workspacepkg.LifecycleReport, out, errOut io.Writer) {
	if report.TaskID != "" {
		fmt.Fprintf(out, "%s task: %s\n", operation, report.TaskID)
	}
	for _, result := range report.Results {
		if result.Error != "" {
			fmt.Fprintf(errOut, "%s %s: %s (%s)\n", operation, result.ID, result.Status, result.Error)
		} else {
			fmt.Fprintf(out, "%s %s: %s\n", operation, result.ID, result.Status)
		}
	}
	for _, id := range report.Pending {
		fmt.Fprintf(out, "%s pending: %s\n", operation, id)
	}
}
