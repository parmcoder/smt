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

	"github.com/charmbracelet/x/term"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/checks"
	"github.com/parmcoder/smt/internal/commit"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/contracts"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
	"github.com/parmcoder/smt/internal/operations"
	"github.com/parmcoder/smt/internal/prereq"
	"github.com/parmcoder/smt/internal/scaffold"
	"github.com/parmcoder/smt/internal/tui"
	"github.com/sirupsen/logrus"
)

const (
	exitOK         = 0
	exitUsage      = 1
	exitValidation = 2
	exitInternal   = 3
)

func main() {
	setup := len(os.Args) >= 2 && os.Args[1] == "init"
	if setup && len(os.Args) > 3 {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	if (len(os.Args) == 1 || setup) && isTTY(os.Stdin) && isTTY(os.Stdout) {
		var err error
		if setup {
			destination := "."
			if len(os.Args) == 3 {
				destination = os.Args[2]
			}
			root, _ := os.Getwd()
			err = tui.RunSetupLocal(context.Background(), os.Getenv("NO_COLOR") != "", root, destination)
		} else {
			root, _ := os.Getwd()
			err = tui.RunLocal(context.Background(), os.Getenv("NO_COLOR") != "", root)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "tui:", err)
			os.Exit(exitInternal)
		}
		return
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func isTTY(file *os.File) bool { return file != nil && term.IsTerminal(file.Fd()) }

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
		fmt.Fprintln(errOut, "SMT interactive mode requires a terminal. Run `smt --help` for commands.")
		return exitUsage
	}
	if args[0] == "init" {
		return runInit(args[1:], in, out, errOut)
	}
	if args[0] == "validate-message" {
		return runValidateMessage(args[1:], out, errOut)
	}
	if len(args) >= 2 && args[0] == "review" && (args[1] == "pass" || args[1] == "fail" || args[1] == "close") {
		fmt.Fprintln(errOut, "human review decisions are available only in the SMT TUI")
		printUsage(errOut)
		return exitUsage
	}
	if args[0] != "status" && args[0] != "push" && args[0] != "doctor" && args[0] != "check" && args[0] != "contracts" && args[0] != "ci" && args[0] != "work" && args[0] != "review" && args[0] != "release" {
		printUsage(errOut)
		return exitUsage
	}

	cfg, root, code := loadConfig(errOut)
	if code != exitOK {
		return code
	}
	ctx := context.Background()
	switch args[0] {
	case "status":
		return runStatus(ctx, args[1:], cfg, root, out, errOut)
	case "push":
		return runPush(ctx, args[1:], *cfg, out, errOut)
	case "doctor":
		return runDoctor(ctx, args[1:], cfg, out, errOut)
	case "check":
		return runCheck(ctx, args[1:], *cfg, out, errOut, logger)
	case "contracts":
		if len(args) != 2 || args[1] != "validate" {
			fmt.Fprintln(errOut, "usage: smt contracts validate")
			return exitUsage
		}
		return runContractReport(*cfg, root, false, out, errOut)
	case "ci":
		return runCI(ctx, args[1:], *cfg, root, out, errOut)
	case "work", "review", "release":
		return runAgentRoute(ctx, args, root, out, errOut)
	default:
		printUsage(errOut)
		return exitUsage
	}
}

func runPush(ctx context.Context, args []string, cfg config.Config, out, errOut io.Writer) int {
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
			Provider:  repository.Provider,
		})
	}
	plan, err := git.PlanPush(ctx, targets)
	if err != nil {
		fmt.Fprintf(errOut, "push: %v\n", err)
		return exitValidation
	}
	report, err := git.ExecutePush(ctx, plan, *dryRun)
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
	result, err := scaffold.New(prereq.New()).Init(context.Background(), destination, selection)
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

func runAgentRoute(ctx context.Context, args []string, root string, out, errOut io.Writer) int {
	service := beads.New(root, beads.CommandRunner{})
	write := func(value any, jsonMode bool) int {
		if jsonMode {
			data, err := json.Marshal(value)
			if err != nil {
				fmt.Fprintln(errOut, "agent route: encode failed")
				return exitInternal
			}
			fmt.Fprintln(out, string(data))
			return exitOK
		}
		switch v := value.(type) {
		case []safeIssue:
			for _, issue := range v {
				fmt.Fprintf(out, "%s %s %s state=%s %s\n", issue.ID, issue.Status, issue.Type, issue.ReviewState, issue.Title)
			}
		case safeRecovery:
			fmt.Fprintf(out, "review=%s bug=%s recovery=%s\n", v.ReviewID, v.BugID, v.Recovery)
		case safeReleaseResult:
			fmt.Fprintf(out, "release ready=%t blockers=%d\n", v.Ready, len(v.Blocking))
			for _, blocker := range v.Blocking {
				fmt.Fprintf(out, "blocker %s %s\n", blocker.ID, blocker.Status)
			}
		default:
			fmt.Fprintln(out, "operation complete")
		}
		return exitOK
	}
	if len(args) >= 2 && args[0] == "work" && args[1] == "ready" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			fmt.Fprintln(errOut, "usage: smt work ready [--json]")
			return exitUsage
		}
		result, err := service.ReadyWork(ctx)
		if err != nil {
			fmt.Fprintln(errOut, "work ready: operation failed")
			return exitValidation
		}
		return write(safeIssues(result), len(args) == 3)
	}
	if len(args) >= 2 && args[0] == "review" && args[1] == "list" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			fmt.Fprintln(errOut, "usage: smt review list [--json]")
			return exitUsage
		}
		result, err := service.ListReviews(ctx)
		if err != nil {
			fmt.Fprintln(errOut, "review list: operation failed")
			return exitValidation
		}
		return write(safeIssues(result), len(args) == 3)
	}
	if len(args) >= 2 && args[0] == "release" && args[1] == "check" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			fmt.Fprintln(errOut, "usage: smt release check [--json]")
			return exitUsage
		}
		result, err := service.ReleaseReadiness(ctx)
		if err != nil {
			fmt.Fprintln(errOut, "release check: operation failed")
			return exitValidation
		}
		if code := write(safeRelease(result), len(args) == 3); code != exitOK {
			return code
		}
		if !result.Ready {
			return exitValidation
		}
		return exitOK
	}
	if len(args) >= 3 && args[0] == "review" && (args[1] == "pass" || args[1] == "fail" || args[1] == "close") {
		fmt.Fprintln(errOut, "human review decisions are available only in the SMT TUI")
		return exitUsage
	}
	if len(args) >= 3 && args[0] == "review" && args[1] == "requeue" {
		jsonMode := false
		if len(args) == 4 && args[3] == "--json" {
			jsonMode = true
		} else if len(args) != 3 {
			fmt.Fprintln(errOut, "usage: smt review requeue REVIEW [--json]")
			return exitUsage
		}
		result, err := service.RequeueAfterFix(ctx, args[2])
		if err != nil {
			write(safeRecovery{result.ReviewID, result.BugID, result.Recovery}, jsonMode)
			fmt.Fprintln(errOut, "review requeue: operation failed")
			return exitValidation
		}
		return write(safeRecovery{result.ReviewID, result.BugID, result.Recovery}, jsonMode)
	}
	if len(args) >= 3 && args[0] == "review" && args[1] == "queue" {
		flags := flag.NewFlagSet("review queue", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		handoff := flags.String("handoff", "", "")
		evidence := flags.String("evidence", "", "")
		jsonMode := flags.Bool("json", false, "")
		if err := flags.Parse(args[3:]); err != nil || flags.NArg() != 0 {
			fmt.Fprintln(errOut, "usage: smt review queue FEATURE --handoff PATH --evidence PATH [--json]")
			return exitUsage
		}
		result, err := service.QueueReview(ctx, args[2], *handoff, *evidence)
		if err != nil {
			write(safeQueue(result), *jsonMode)
			fmt.Fprintln(errOut, "review queue: operation failed")
			return exitValidation
		}
		return write(safeQueue(result), *jsonMode)
	}
	printUsage(errOut)
	return exitUsage
}

type safeIssue struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Type        string   `json:"type"`
	Labels      []string `json:"labels"`
	ReviewState string   `json:"review_state,omitempty"`
}
type safeRecovery struct {
	ReviewID string `json:"review_id"`
	BugID    string `json:"bug_id,omitempty"`
	Recovery string `json:"recovery,omitempty"`
}
type safeReleaseResult struct {
	Ready    bool        `json:"ready"`
	Blocking []safeIssue `json:"blocking"`
}

func safeQueue(value beads.QueueResult) safeRecovery {
	return safeRecovery{value.ReviewID, "", value.Recovery}
}

func safeIssues(issues []beads.Issue) []safeIssue {
	result := make([]safeIssue, len(issues))
	for i, issue := range issues {
		result[i] = safeIssue{issue.ID, issue.Title, issue.Status, issue.Type, issue.Labels, issue.ReviewState}
	}
	return result
}
func safeRelease(value beads.ReleaseReadiness) safeReleaseResult {
	return safeReleaseResult{value.Ready, safeIssues(value.Blocking)}
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

func runStatus(ctx context.Context, args []string, cfg *config.Config, root string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "write JSON output")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: smt status [--json]")
		return exitUsage
	}
	entries, err := operations.New(*cfg).Status(ctx)
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

func runDoctor(ctx context.Context, args []string, cfg *config.Config, out, errOut io.Writer) int {
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
		return git.Inspect(ctx, git.Repository{Dir: dir})
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

func runCheck(ctx context.Context, args []string, cfg config.Config, out, errOut io.Writer, logger *logrus.Logger) int {
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
			changed, err = git.ChangedFiles(ctx, git.Repository{ID: repository.ID, Dir: repository.Path})
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

func runCI(_ context.Context, args []string, cfg config.Config, root string, out, errOut io.Writer) int {
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
	fmt.Fprintln(out, "usage: smt init [PATH] | push [--dry-run] | validate-message FILE | status [--json] | doctor | check --profile PROFILE | contracts validate | ci audit | ci contracts bump --id ID [--apply]")
}
