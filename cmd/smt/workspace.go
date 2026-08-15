package main

import (
	"context"
	"fmt"
	"io"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
	"github.com/sirupsen/logrus"
)

type lifecycleBeadsService interface{ workspacepkg.BeadsLifecycle }

var newLifecycleBeadsService = func(root string) lifecycleBeadsService { return beads.New(root, beads.CommandRunner{}) }

var newVerboseLifecycleBeadsService = func(root string, verbose io.Writer) lifecycleBeadsService {
	return beads.NewWithVerbose(root, beads.CommandRunner{}, verbose)
}

func runRepositoryPrepare(ctx context.Context, cfg config.Config, root string, service lifecycleBeadsService, runner git.Runner, out, errOut io.Writer, logger *logrus.Logger, verbose bool) int {
	report, err := workspacepkg.Prepare(ctx, cfg, root, runner, service)
	writeLifecycleReport("prepare", report, out, errOut)
	return reportLifecycleResult("prepare", report, err, errOut, logger, verbose)
}
func runRepositorySwitch(ctx context.Context, cfg config.Config, root, id string, service lifecycleBeadsService, runner git.Runner, out, errOut io.Writer, logger *logrus.Logger, verbose bool) int {
	report, err := workspacepkg.Switch(ctx, cfg, root, id, runner, service)
	writeLifecycleReport("switch", report, out, errOut)
	return reportLifecycleResult("switch", report, err, errOut, logger, verbose)
}
func writeLifecycleReport(operation string, report workspacepkg.LifecycleReport, out, errOut io.Writer) {
	if report.TaskID != "" {
		fmt.Fprintf(out, "%s task: %s\n", operation, report.TaskID)
	}
	for _, result := range report.Results {
		fmt.Fprintf(out, "%s %s: %s\n", operation, result.ID, result.Status)
	}
	for _, id := range report.Pending {
		fmt.Fprintf(out, "%s pending: %s\n", operation, id)
	}
}

func reportLifecycleResult(operation string, report workspacepkg.LifecycleReport, err error, errOut io.Writer, logger *logrus.Logger, verbose bool) int {
	if err == nil {
		return exitOK
	}
	if verbose && logger != nil {
		fields := logrus.Fields{
			"command":   operation,
			"status":    "failed",
			"exit_code": exitValidation,
		}
		if report.TaskID != "" {
			fields["task_id"] = report.TaskID
		}
		logger.WithError(err).WithFields(fields).Error("lifecycle command failed")
		return exitValidation
	}
	fmt.Fprintln(errOut, err)
	return exitValidation
}
