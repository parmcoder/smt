package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parmcoder/smt/internal/checks"
	"github.com/parmcoder/smt/internal/commit"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/contracts"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
	"github.com/parmcoder/smt/internal/operations"
)

const (
	exitOK         = 0
	exitUsage      = 1
	exitValidation = 2
	exitInternal   = 3
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		printUsage(errOut)
		return exitUsage
	}
	if args[0] == "validate-message" {
		return runValidateMessage(args[1:], out, errOut)
	}

	cfg, root, code := loadConfig(errOut)
	if code != exitOK {
		return code
	}
	ctx := context.Background()
	runner := git.ExecRunner{}

	switch args[0] {
	case "status":
		return runStatus(ctx, args[1:], cfg, root, runner, out, errOut)
	case "doctor":
		return runDoctor(ctx, args[1:], cfg, runner, out, errOut)
	case "check":
		return runCheck(ctx, args[1:], *cfg, runner, out, errOut)
	case "contracts":
		if len(args) != 2 || args[1] != "validate" {
			fmt.Fprintln(errOut, "usage: smt contracts validate")
			return exitUsage
		}
		return runContractReport(*cfg, root, false, out, errOut)
	case "ci":
		return runCI(ctx, args[1:], *cfg, root, runner, out, errOut)
	default:
		printUsage(errOut)
		return exitUsage
	}
}

func runValidateMessage(args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(errOut, "usage: smt validate-message FILE")
		return exitUsage
	}
	message, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(errOut, "read message: %v\n", err)
		return exitInternal
	}
	cfg, _, code := loadConfig(errOut)
	if code != exitOK {
		return code
	}
	if err := commit.ValidateMessage(string(message), commit.Policy{Types: cfg.Commit.Types, Scopes: cfg.Commit.Scopes}); err != nil {
		fmt.Fprintln(errOut, err)
		return exitValidation
	}
	fmt.Fprintln(out, "valid commit message")
	return exitOK
}

func loadConfig(errOut io.Writer) (*config.Config, string, int) {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(errOut, "resolve working directory: %v\n", err)
		return nil, "", exitInternal
	}
	cfg, err := config.Load(filepath.Join(root, "smt.yaml"))
	if err != nil {
		fmt.Fprintf(errOut, "configuration error: %v\n", err)
		return nil, "", exitInternal
	}
	return cfg, root, exitOK
}

type statusOutput struct {
	Repositories []operations.Entry `json:"repositories"`
	Profiles     []string           `json:"profiles"`
	Contracts    contractCounts     `json:"contracts"`
}

type contractCounts struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

func runStatus(ctx context.Context, args []string, cfg *config.Config, root string, runner git.Runner, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "write JSON output")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: smt status [--json]")
		return exitUsage
	}
	entries, err := operations.New(*cfg, runner).Status(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "status: %v\n", err)
		return exitInternal
	}
	contractService, err := newContracts(root, *cfg)
	if err != nil {
		fmt.Fprintf(errOut, "contract status: %v\n", err)
		return exitInternal
	}
	report, err := contractService.Validate()
	if err != nil {
		fmt.Fprintf(errOut, "contract status: %v\n", err)
		return exitInternal
	}
	result := statusOutput{Repositories: entries, Profiles: profileNames(*cfg), Contracts: countFindings(report)}
	if *jsonOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(errOut, "encode status: %v\n", err)
			return exitInternal
		}
		return exitOK
	}
	fmt.Fprintf(out, "profiles: %s\n", strings.Join(result.Profiles, ", "))
	fmt.Fprintf(out, "contracts: errors=%d warnings=%d\n", result.Contracts.Errors, result.Contracts.Warnings)
	for _, entry := range entries {
		state := "uninitialized"
		if entry.Initialized {
			state = "clean"
			if entry.Dirty {
				state = "dirty"
			}
			if entry.Detached {
				state = "detached"
			}
		}
		if entry.Error != "" {
			state += " error=" + entry.Error
		}
		fmt.Fprintf(out, "%s: %s hook=%s\n", entry.ID, state, entry.HookStatus)
	}
	return exitOK
}

func runDoctor(ctx context.Context, args []string, cfg *config.Config, runner git.Runner, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: smt doctor")
		return exitUsage
	}
	doctor := operations.NewDoctorWithHookInspector(*cfg, exec.LookPath, func(name string) bool {
		_, ok := os.LookupEnv(name)
		return ok
	}, func(ctx context.Context, dir string) (git.State, error) {
		return git.Inspect(ctx, runner, git.Repository{Dir: dir})
	}, hooks.InspectCommitMsg)
	result, err := doctor.Run(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "doctor: %v\n", err)
		return exitInternal
	}
	failed := false
	for _, check := range result.Checks {
		fmt.Fprintf(out, "%s: %s - %s\n", check.ID, check.Status, check.Message)
		failed = failed || check.Status == "error"
	}
	if failed {
		return exitValidation
	}
	return exitOK
}

func runCheck(ctx context.Context, args []string, cfg config.Config, runner git.Runner, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	profile := flags.String("profile", "", "check profile")
	repoID := flags.String("repo", "", "repository ID")
	allowMutation := flags.Bool("allow-worktree-mutation", false, "allow checks that mutate the worktree")
	dryRun := flags.Bool("dry-run", false, "validate without running checks")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *profile == "" {
		fmt.Fprintln(errOut, "usage: smt check --profile hook|submit|ci-parity [--repo ID] [--allow-worktree-mutation] [--dry-run]")
		return exitUsage
	}
	selected := cfg.Repositories
	if *repoID != "" {
		selected = nil
		for _, repository := range cfg.Repositories {
			if repository.ID == *repoID {
				selected = append(selected, repository)
			}
		}
		if selected == nil {
			fmt.Fprintf(errOut, "unknown repository %q\n", *repoID)
			return exitUsage
		}
	}
	executor := commandExecutor{}
	for _, repository := range selected {
		if _, ok := repository.Profiles[*profile]; !ok {
			fmt.Fprintf(errOut, "repository %q has no check profile %q\n", repository.ID, *profile)
			return exitUsage
		}
		var changed []string
		if !*dryRun {
			var err error
			changed, err = git.ChangedFiles(ctx, runner, git.Repository{ID: repository.ID, Dir: repository.Path})
			if err != nil {
				fmt.Fprintf(errOut, "check %s: %v\n", repository.ID, err)
				return exitInternal
			}
		}
		if err := checks.RunProfile(ctx, executor, repository, *profile, changed, *allowMutation, *dryRun); err != nil {
			fmt.Fprintln(errOut, err)
			return exitValidation
		}
		fmt.Fprintf(out, "%s: %s checks passed\n", repository.ID, *profile)
	}
	return exitOK
}

func runContractReport(cfg config.Config, root string, audit bool, out, errOut io.Writer) int {
	service, err := newContracts(root, cfg)
	if err != nil {
		fmt.Fprintf(errOut, "contracts: %v\n", err)
		return exitInternal
	}
	var report contracts.Report
	if audit {
		report, err = service.Audit()
	} else {
		report, err = service.Validate()
	}
	if err != nil {
		fmt.Fprintf(errOut, "contracts: %v\n", err)
		return exitInternal
	}
	printFindings(out, report)
	if report.HasErrors() {
		return exitValidation
	}
	return exitOK
}

func runCI(_ context.Context, args []string, cfg config.Config, root string, _ git.Runner, out, errOut io.Writer) int {
	if len(args) == 1 && args[0] == "audit" {
		return runContractReport(cfg, root, true, out, errOut)
	}
	if len(args) < 3 || args[0] != "contracts" || args[1] != "bump" {
		fmt.Fprintln(errOut, "usage: smt ci audit | smt ci contracts bump --id ID [--apply]")
		return exitUsage
	}
	flags := flag.NewFlagSet("ci contracts bump", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	id := flags.String("id", "", "reference contract ID")
	apply := flags.Bool("apply", false, "apply the replacement")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *id == "" {
		fmt.Fprintln(errOut, "usage: smt ci contracts bump --id ID [--apply]")
		return exitUsage
	}
	service, err := newContracts(root, cfg)
	if err != nil {
		fmt.Fprintf(errOut, "contracts: %v\n", err)
		return exitInternal
	}
	plan, err := service.PlanBump(*id)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitValidation
	}
	fmt.Fprintf(out, "contract %s (%s)\n--- before\n%s--- after\n%s", plan.ContractID, plan.Path, plan.Before, plan.After)
	if !*apply {
		return exitOK
	}
	if err := service.Apply(plan, true); err != nil {
		fmt.Fprintln(errOut, err)
		return exitValidation
	}
	fmt.Fprintln(out, "applied")
	return exitOK
}

func newContracts(root string, cfg config.Config) (*contracts.Service, error) {
	return contracts.New(root, cfg.Contracts)
}

func countFindings(report contracts.Report) contractCounts {
	counts := contractCounts{}
	for _, finding := range report.Findings {
		if finding.Severity == contracts.SeverityError {
			counts.Errors++
		} else {
			counts.Warnings++
		}
	}
	return counts
}

func profileNames(cfg config.Config) []string {
	seen := map[string]struct{}{}
	for _, repository := range cfg.Repositories {
		for profile := range repository.Profiles {
			seen[profile] = struct{}{}
		}
	}
	profiles := make([]string, 0, len(seen))
	for profile := range seen {
		profiles = append(profiles, profile)
	}
	sort.Strings(profiles)
	return profiles
}

func printFindings(out io.Writer, report contracts.Report) {
	if len(report.Findings) == 0 {
		fmt.Fprintln(out, "no contract findings")
		return
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(out, "%s [%s] %s: %s (%s)\n", finding.ContractID, finding.Severity, finding.Type, finding.Message, finding.Path)
	}
}

type commandExecutor struct{}

func (commandExecutor) Run(ctx context.Context, dir string, argv []string, repositoryID string) error {
	if len(argv) == 0 {
		return fmt.Errorf("repository %s: empty command", repositoryID)
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = dir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return fmt.Errorf("%w: %s", err, message)
		}
		return err
	}
	return nil
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "usage: smt validate-message FILE | status [--json] | doctor | check --profile PROFILE | contracts validate | ci audit | ci contracts bump --id ID [--apply]")
}
