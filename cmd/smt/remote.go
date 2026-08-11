package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	providerpkg "github.com/parmcoder/smt/internal/provider"
	remotepkg "github.com/parmcoder/smt/internal/remote"
)

func runRemoteProvision(ctx context.Context, cfg config.Config, root string, runner git.Runner, dryRun, jsonOutput bool, out, errOut io.Writer) int {
	report, err := remotepkg.Provision(ctx, cfg, root, runner, func(name string, settings config.ProviderConfig, token string) (providerpkg.ProjectProvider, error) {
		projects, _, factoryErr := providerpkg.NewConfigured(name, settings, token, nil)
		return projects, factoryErr
	}, func(name string) string {
		return os.Getenv("SMT_" + strings.ToUpper(name) + "_TOKEN")
	}, dryRun)
	if jsonOutput {
		if encodeErr := json.NewEncoder(out).Encode(report); encodeErr != nil {
			fmt.Fprintf(errOut, "remote provision: encode report: %v\n", encodeErr)
			return exitInternal
		}
	} else {
		writeRemoteProvisionHuman(report, out)
	}
	if err != nil {
		fmt.Fprintf(errOut, "%v\n", err)
		return exitValidation
	}
	return exitOK
}

func writeRemoteProvisionHuman(report remotepkg.Report, out io.Writer) {
	if report.DryRun {
		fmt.Fprintln(out, "remote provision plan:")
	} else {
		fmt.Fprintln(out, "remote provision:")
	}
	for _, project := range report.Projects {
		if project.Error != "" {
			fmt.Fprintf(out, "- %s: %s (%s)\n", project.ID, project.Status, project.Error)
			continue
		}
		fmt.Fprintf(out, "- %s: %s\n", project.ID, project.Status)
	}
	if len(report.Configured) > 0 {
		fmt.Fprintf(out, "configured: %s\n", strings.Join(report.Configured, ", "))
	}
	if len(report.Pending) > 0 {
		fmt.Fprintf(out, "pending: %s\n", strings.Join(report.Pending, ", "))
	}
	if report.WiringError != "" {
		fmt.Fprintf(out, "wiring error: %s\n", report.WiringError)
	}
}
