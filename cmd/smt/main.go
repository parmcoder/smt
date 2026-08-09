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
	"time"

	applypkg "github.com/parmcoder/smt/internal/apply"
	"github.com/parmcoder/smt/internal/blueprint"
	"github.com/parmcoder/smt/internal/checks"
	"github.com/parmcoder/smt/internal/commit"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/contracts"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
	"github.com/parmcoder/smt/internal/operations"
	"github.com/parmcoder/smt/internal/scaffold"
	"github.com/sirupsen/logrus"
)

const (
	exitOK         = 0
	exitUsage      = 1
	exitValidation = 2
	exitInternal   = 3
)

var newInputIsTerminal = func(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

var newApplyService = func() applypkg.Service { return applypkg.New() }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	return runWithInput(args, os.Stdin, out, errOut)
}

func runWithInput(args []string, in io.Reader, out, errOut io.Writer) int {
	verbose := len(args) > 0 && args[0] == "--verbose"
	if verbose {
		args = args[1:]
	}
	command := ""
	if len(args) > 0 {
		command = args[0]
	}
	logger := newRunLogger(verbose, errOut)
	started := time.Now()
	code := runCommand(args, in, out, errOut, logger)
	if verbose {
		logger.WithFields(logrus.Fields{
			"command":     command,
			"status":      commandStatus(code),
			"exit_code":   code,
			"duration_ms": time.Since(started).Milliseconds(),
		}).Debug("command finished")
	}
	return code
}

func newRunLogger(verbose bool, errOut io.Writer) *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(errOut)
	formatter := &logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339,
		PadLevelText:    true,
		DisableQuote:    true,
	}
	if os.Getenv("CLICOLOR_FORCE") == "1" {
		formatter.ForceColors = true
	}
	if os.Getenv("NO_COLOR") != "" {
		formatter.DisableColors = true
		formatter.ForceColors = false
	}
	logger.SetFormatter(formatter)
	if verbose {
		logger.SetLevel(logrus.DebugLevel)
	}
	return logger
}

func runCommand(args []string, in io.Reader, out, errOut io.Writer, logger *logrus.Logger) int {
	if len(args) == 0 {
		printUsage(errOut)
		return exitUsage
	}
	if args[0] == "init" {
		return runInit(args[1:], in, out, errOut)
	}
	if args[0] == "new" {
		return runNew(args[1:], in, out, errOut)
	}
	if args[0] == "apply" {
		return runApply(args[1:], out, errOut)
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
	case "push":
		return runPush(ctx, args[1:], *cfg, runner, out, errOut)
	case "worktree":
		return runWorktree(ctx, args[1:], *cfg, root, runner, out, errOut)
	case "doctor":
		return runDoctor(ctx, args[1:], cfg, runner, out, errOut)
	case "check":
		return runCheck(ctx, args[1:], *cfg, runner, out, errOut, logger)
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

func runApply(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "./smt.yaml", "configuration file")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(errOut, "usage: smt apply [--config FILE] PATH")
		return exitUsage
	}
	raw, err := os.ReadFile(*configPath)
	if err != nil {
		fmt.Fprintf(errOut, "apply: read config: %v\n", err)
		return exitValidation
	}
	cfg, err := config.LoadBytes(raw, *configPath)
	if err != nil {
		fmt.Fprintf(errOut, "apply: %v\n", err)
		return exitValidation
	}
	if err := applypkg.ValidateBlueprint(*cfg); err != nil {
		fmt.Fprintf(errOut, "apply: %v\n", err)
		return exitValidation
	}
	service := newApplyService()
	service.Config = *cfg
	if err := service.Apply(context.Background(), flags.Arg(0), raw); err != nil {
		fmt.Fprintf(errOut, "apply: %v\n", err)
		return exitValidation
	}
	fmt.Fprintln(out, "applied blueprint")
	return exitOK
}

func runNew(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(errOut, "usage: smt new [FILE]")
		return exitUsage
	}
	destination := "./smt.yaml"
	if len(args) == 1 {
		destination = args[0]
	}
	if !newInputIsTerminal(in) {
		fmt.Fprintln(errOut, "new: interactive terminal input is required")
		return exitUsage
	}
	_, err := blueprint.Create(in, out, destination)
	if err != nil {
		fmt.Fprintf(errOut, "new: %v\n", err)
		return exitValidation
	}
	return exitOK
}

func runPush(ctx context.Context, args []string, cfg config.Config, runner git.Runner, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("push", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dryRun := flags.Bool("dry-run", false, "validate and print the push plan")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: smt push [--dry-run]")
		return exitUsage
	}
	targets := make([]git.PushTarget, 0, len(cfg.Repositories))
	for _, repository := range cfg.Repositories {
		targets = append(targets, git.PushTarget{
			Repository: git.Repository{
				ID:     repository.ID,
				Dir:    repository.Path,
				IsRoot: filepath.Clean(repository.Path) == ".",
			},
			RemoteURL: repository.Remote.URL,
		})
	}
	plan, err := git.PlanPush(ctx, runner, targets)
	if err != nil {
		fmt.Fprintf(errOut, "push: %v\n", err)
		return exitValidation
	}
	report, err := git.ExecutePush(ctx, runner, plan, *dryRun)
	if report.DryRun {
		fmt.Fprintln(out, "push plan:")
		for _, step := range report.Planned {
			fmt.Fprintf(out, "%s: %s\n", step.Repository.ID, step.Branch)
		}
		return exitOK
	}
	for _, step := range report.Pushed {
		fmt.Fprintf(out, "pushed %s: %s\n", step.Repository.ID, step.Branch)
	}
	if err == nil {
		return exitOK
	}
	for _, step := range report.Pending {
		fmt.Fprintf(out, "pending %s: %s\n", step.Repository.ID, step.Branch)
	}
	fmt.Fprintf(errOut, "push: %v\n", err)
	return exitValidation
}

func runWorktree(ctx context.Context, args []string, cfg config.Config, root string, runner git.Runner, out, errOut io.Writer) int {
	destination, branch, dryRun, ok := parseWorktreeArgs(args)
	if !ok {
		fmt.Fprintln(errOut, "usage: smt worktree add PATH --branch NAME [--dry-run]")
		return exitUsage
	}
	targets := make([]git.WorktreeTarget, 0, len(cfg.Repositories))
	for _, repository := range cfg.Repositories {
		dir := root
		if filepath.Clean(repository.Path) != "." {
			dir = filepath.Join(root, repository.Path)
		}
		targets = append(targets, git.WorktreeTarget{
			Repository: git.Repository{
				ID:     repository.ID,
				Dir:    dir,
				IsRoot: filepath.Clean(repository.Path) == ".",
			},
			Path: repository.Path,
		})
	}
	plan, err := git.PlanWorktree(ctx, runner, targets, destination, branch)
	if err != nil {
		fmt.Fprintf(errOut, "worktree: %v\n", err)
		return exitValidation
	}
	report, err := git.ExecuteWorktree(ctx, runner, plan, dryRun)
	if report.DryRun {
		fmt.Fprintln(out, "worktree plan:")
		for _, step := range report.Planned {
			fmt.Fprintf(out, "%s: %s\n", step.Repository.ID, step.Destination)
		}
		return exitOK
	}
	for _, step := range report.Created {
		fmt.Fprintf(out, "created %s: %s\n", step.Repository.ID, step.Destination)
	}
	if err == nil {
		return exitOK
	}
	for _, step := range report.Pending {
		fmt.Fprintf(out, "pending %s: %s\n", step.Repository.ID, step.Destination)
	}
	fmt.Fprintf(errOut, "worktree: %v\n", err)
	return exitValidation
}

func parseWorktreeArgs(args []string) (destination, branch string, dryRun, ok bool) {
	if len(args) == 0 || args[0] != "add" {
		return "", "", false, false
	}
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--dry-run":
			dryRun = true
		case "--branch":
			if index+1 >= len(args) || args[index+1] == "" {
				return "", "", false, false
			}
			branch = args[index+1]
			index++
		default:
			if strings.HasPrefix(args[index], "-") || destination != "" {
				return "", "", false, false
			}
			destination = args[index]
		}
	}
	return destination, branch, dryRun, destination != "" && branch != ""
}

func runInit(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) > 1 {
		fmt.Fprintln(errOut, "usage: smt init [PATH]")
		return exitUsage
	}
	destination := "."
	if len(args) == 1 {
		destination = args[0]
	}
	selection, err := scaffold.Prompt(in, out)
	if err != nil {
		fmt.Fprintf(errOut, "init: %v\n", err)
		return exitUsage
	}
	result, err := scaffold.New(git.ExecRunner{}).Init(context.Background(), destination, selection)
	if err != nil {
		fmt.Fprintf(errOut, "init: %v\n", err)
		return exitValidation
	}
	fmt.Fprintf(out, "initialized workspace %s (%s)\n", result.Destination, strings.Join(result.Repositories, ", "))
	return exitOK
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

func runCheck(ctx context.Context, args []string, cfg config.Config, runner git.Runner, out, errOut io.Writer, logger *logrus.Logger) int {
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
	executor := commandExecutor{logger: logger.WithField("profile", *profile)}
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

type commandExecutor struct {
	logger *logrus.Entry
}

func (e commandExecutor) Run(ctx context.Context, dir string, argv []string, repositoryID string) error {
	if len(argv) == 0 {
		return fmt.Errorf("repository %s: empty command", repositoryID)
	}
	program := filepath.Base(argv[0])
	entry := e.logger
	if entry != nil {
		entry = entry.WithFields(logrus.Fields{
			"repository": repositoryID,
			"program":    program,
		})
		entry.Debug("check started")
	}
	started := time.Now()
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = dir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err := command.Run()
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	status := "success"
	if err != nil {
		status = "failed"
	}
	if entry != nil {
		entry.WithFields(logrus.Fields{
			"status":       status,
			"exit_code":    exitCode,
			"duration_ms":  time.Since(started).Milliseconds(),
			"stderr_bytes": len(stderr.Bytes()),
		}).Debug(checkResultMessage(err))
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return fmt.Errorf("%w: %s", err, message)
		}
		return err
	}
	return nil
}

func commandStatus(code int) string {
	if code == exitOK {
		return "success"
	}
	return "failed"
}

func checkResultMessage(err error) string {
	if err == nil {
		return "check completed"
	}
	return "check failed"
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "usage: smt new [FILE] | apply [--config FILE] PATH | init [PATH] | push [--dry-run] | worktree add PATH --branch NAME [--dry-run] | validate-message FILE | status [--json] | doctor | check --profile PROFILE | contracts validate | ci audit | ci contracts bump --id ID [--apply]")
}
