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
	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/blueprint"
	"github.com/parmcoder/smt/internal/checks"
	"github.com/parmcoder/smt/internal/commit"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/contracts"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
	"github.com/parmcoder/smt/internal/operations"
	"github.com/parmcoder/smt/internal/tui"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
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
var reviewIsInteractive = func(in io.Reader, out io.Writer) bool {
	a, ok := in.(*os.File)
	if !ok {
		return false
	}
	b, ok := out.(*os.File)
	if !ok {
		return false
	}
	ai, e := a.Stat()
	bi, f := b.Stat()
	return e == nil && f == nil && ai.Mode()&os.ModeCharDevice != 0 && bi.Mode()&os.ModeCharDevice != 0
}
var runReviewTUI = func(ctx context.Context, noColor bool, root string) error { return tui.RunLocal(ctx, noColor, root) }

type agentService interface {
	ReadyWork(context.Context) ([]beads.Issue, error)
	ListReviews(context.Context) ([]beads.Issue, error)
	QueueReview(context.Context, string, string, string) (beads.QueueResult, error)
	RequeueAfterFix(context.Context, string) (beads.Recovery, error)
	ReleaseReadiness(context.Context) (beads.ReleaseReadiness, error)
}

var newBeadsService = func(root string) agentService { return beads.New(root, beads.CommandRunner{}) }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errOut io.Writer) int {
	return runWithInput(args, os.Stdin, out, errOut)
}

func runWithInput(args []string, in io.Reader, out, errOut io.Writer) int {
	args, verbose := withoutVerbose(args)
	root := newRootCommand(in, out, errOut, verbose)
	root.SetArgs(args)
	started := time.Now()
	if err := root.Execute(); err != nil {
		if code, ok := err.(commandExitError); ok {
			return int(code)
		}
		fmt.Fprintf(errOut, "error: %v\n", err)
		fmt.Fprint(errOut, root.UsageString())
		if verbose {
			command := ""
			if len(args) > 0 {
				command = args[0]
			}
			newRunLogger(true, errOut).WithFields(logrus.Fields{
				"command":     command,
				"status":      commandStatus(exitUsage),
				"exit_code":   exitUsage,
				"duration_ms": time.Since(started).Milliseconds(),
			}).Debug("command finished")
		}
		return exitUsage
	}
	return exitOK
}

type commandExitError int

func (code commandExitError) Error() string { return "command failed" }

func withoutVerbose(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	verbose := false
	for _, arg := range args {
		if arg == "--verbose" {
			verbose = true
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, verbose
}

func newRootCommand(in io.Reader, out, errOut io.Writer, verbose bool) *cobra.Command {
	root := &cobra.Command{
		Use:           "smt",
		Short:         "Sanovy Mono Tool",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	root.PersistentFlags().Bool("verbose", false, "write diagnostic command details to stderr")
	root.AddGroup(
		&cobra.Group{ID: "getting-started", Title: "Getting Started"},
		&cobra.Group{ID: "workspace", Title: "Workspace"},
		&cobra.Group{ID: "review-workflow", Title: "Review Workflow"},
		&cobra.Group{ID: "developer-tools", Title: "Developer Tools"},
	)
	root.SetHelpCommandGroupID("developer-tools")
	root.SetCompletionCommandGroupID("developer-tools")

	leaf := func(use, short, groupID string, path ...string) *cobra.Command {
		return &cobra.Command{
			Use:                use,
			Short:              short,
			GroupID:            groupID,
			DisableFlagParsing: true,
			RunE: func(_ *cobra.Command, args []string) error {
				return commandExitError(runCommandWithVerbose(append(path, args...), in, out, errOut, verbose))
			},
		}
	}
	newCommand := leaf("new [FILE]", "Create a workspace blueprint", "getting-started", "new")
	applyCommand := leaf("apply [--config FILE] PATH", "Apply a workspace blueprint", "getting-started", "apply")
	initCommand := leaf("init [PATH]", "Show workspace initialization guidance", "getting-started", "init")
	pushCommand := leaf("push [--dry-run]", "Push configured repositories", "workspace", "push")
	worktreeCommand := leaf("worktree", "Manage linked worktrees", "workspace", "worktree")
	worktreeCommand.AddCommand(leaf("add PATH --branch NAME [--dry-run]", "Create linked worktrees", "", "worktree", "add"))
	statusCommand := leaf("status [--json]", "Show workspace status", "workspace", "status")
	doctorCommand := leaf("doctor", "Check local readiness", "workspace", "doctor")
	validateCommand := leaf("validate-message FILE", "Validate a commit message", "developer-tools", "validate-message")
	checkCommand := leaf("check --profile PROFILE", "Run a check profile", "developer-tools", "check")
	contractsCommand := leaf("contracts", "Inspect reusable contracts", "developer-tools", "contracts")
	contractsCommand.AddCommand(leaf("validate", "Validate contracts", "", "contracts", "validate"))
	ciCommand := leaf("ci", "Run CI-parity tools", "developer-tools", "ci")
	ciContractsCommand := leaf("contracts", "Manage CI contracts", "", "ci", "contracts")
	ciContractsCommand.AddCommand(leaf("bump --id ID [--apply]", "Plan or apply a contract bump", "", "ci", "contracts", "bump"))
	ciCommand.AddCommand(leaf("audit", "Audit CI parity", "", "ci", "audit"), ciContractsCommand)
	workCommand := leaf("work", "Manage work items", "review-workflow", "work")
	workCommand.AddCommand(leaf("ready [--json]", "List ready work", "", "work", "ready"))
	reviewCommand := leaf("review", "Open the review terminal interface", "review-workflow", "review")
	reviewCommand.AddCommand(
		leaf("list [--json]", "List queued reviews", "", "review", "list"),
		leaf("queue FEATURE --handoff PATH --evidence PATH [--json]", "Queue a review", "", "review", "queue"),
		leaf("requeue REVIEW [--json]", "Requeue a review", "", "review", "requeue"),
		leaf("pass", "Reserved for the review TUI", "", "review", "pass"),
		leaf("fail", "Reserved for the review TUI", "", "review", "fail"),
		leaf("close", "Reserved for the review TUI", "", "review", "close"),
	)
	releaseCommand := leaf("release", "Check release readiness", "review-workflow", "release")
	releaseCommand.AddCommand(leaf("check [--json]", "Check release readiness", "", "release", "check"))
	root.AddCommand(newCommand, applyCommand, initCommand, pushCommand, worktreeCommand, statusCommand, doctorCommand, validateCommand, checkCommand, contractsCommand, ciCommand, workCommand, reviewCommand, releaseCommand)
	return root
}

func runCommandWithVerbose(args []string, in io.Reader, out, errOut io.Writer, verbose bool) int {
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
		printUsage(out)
		return exitOK
	}
	if args[0] == "init" {
		return runInit(args[1:], in, out, errOut)
	}
	if args[0] == "review" && len(args) == 1 {
		if !reviewIsInteractive(in, out) {
			fmt.Fprintln(errOut, "review: interactive terminal input and output are required")
			return exitUsage
		}
		root, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(errOut, "review: resolve working directory: %v\n", err)
			return exitInternal
		}
		if err := runReviewTUI(context.Background(), os.Getenv("NO_COLOR") != "", root); err != nil {
			fmt.Fprintln(errOut, "review: terminal interface failed")
			return exitInternal
		}
		return exitOK
	}
	if args[0] == "review" && len(args) >= 2 && (args[1] == "pass" || args[1] == "fail" || args[1] == "close") {
		fmt.Fprintln(errOut, "human review decisions are available only in the SMT TUI")
		printUsage(errOut)
		return exitUsage
	}
	if args[0] == "work" || args[0] == "review" || args[0] == "release" {
		return runAgentRoute(context.Background(), args, ".", out, errOut)
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
	fmt.Fprintln(out, "smt init no longer creates a workspace; run smt new [FILE], review smt.yaml, then run smt apply [--config FILE] PATH")
	return exitOK
}

func runAgentRoute(ctx context.Context, args []string, root string, out, errOut io.Writer) int {
	s := newBeadsService(root)
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
		switch x := value.(type) {
		case []safeIssue:
			for _, issue := range x {
				fmt.Fprintf(out, "%s %s %s state=%s %s\n", issue.ID, issue.Status, issue.Type, issue.ReviewState, issue.Title)
			}
		case safeRecovery:
			fmt.Fprintf(out, "review=%s bug=%s recovery=%s\n", x.ReviewID, x.BugID, x.Recovery)
		case safeReleaseResult:
			fmt.Fprintf(out, "release ready=%t blockers=%d\n", x.Ready, len(x.Blocking))
			for _, blocker := range x.Blocking {
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
		xs, e := s.ReadyWork(ctx)
		if e != nil {
			fmt.Fprintln(errOut, "work ready: operation failed")
			return exitValidation
		}
		return write(safeIssues(xs), len(args) == 3)
	}
	if len(args) >= 2 && args[0] == "review" && args[1] == "list" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			fmt.Fprintln(errOut, "usage: smt review list [--json]")
			return exitUsage
		}
		xs, e := s.ListReviews(ctx)
		if e != nil {
			fmt.Fprintln(errOut, "review list: operation failed")
			return exitValidation
		}
		return write(safeIssues(xs), len(args) == 3)
	}
	if len(args) >= 2 && args[0] == "release" && args[1] == "check" {
		if len(args) > 3 || (len(args) == 3 && args[2] != "--json") {
			fmt.Fprintln(errOut, "usage: smt release check [--json]")
			return exitUsage
		}
		r, e := s.ReleaseReadiness(ctx)
		if e != nil {
			fmt.Fprintln(errOut, "release check: operation failed")
			return exitValidation
		}
		if code := write(safeRelease(r), len(args) == 3); code != exitOK {
			return code
		}
		if !r.Ready {
			return exitValidation
		}
		return exitOK
	}
	if len(args) >= 3 && args[0] == "review" && args[1] == "requeue" {
		jsonMode := false
		if len(args) == 4 && args[3] == "--json" {
			jsonMode = true
		} else if len(args) != 3 {
			fmt.Fprintln(errOut, "usage: smt review requeue REVIEW [--json]")
			return exitUsage
		}
		r, e := s.RequeueAfterFix(ctx, args[2])
		if e != nil {
			write(safeRecovery{r.ReviewID, r.BugID, r.Recovery}, jsonMode)
			fmt.Fprintln(errOut, "review requeue: operation failed")
			return exitValidation
		}
		return write(safeRecovery{r.ReviewID, r.BugID, r.Recovery}, jsonMode)
	}
	if len(args) >= 3 && args[0] == "review" && args[1] == "queue" {
		f := flag.NewFlagSet("review queue", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		h := f.String("handoff", "", "")
		e := f.String("evidence", "", "")
		jsonMode := f.Bool("json", false, "")
		if f.Parse(args[3:]) != nil || f.NArg() != 0 {
			fmt.Fprintln(errOut, "usage: smt review queue FEATURE --handoff PATH --evidence PATH [--json]")
			return exitUsage
		}
		r, x := s.QueueReview(ctx, args[2], *h, *e)
		if x != nil {
			write(safeQueue(r), *jsonMode)
			fmt.Fprintln(errOut, "review queue: operation failed")
			return exitValidation
		}
		return write(safeQueue(r), *jsonMode)
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
	return safeRecovery{ReviewID: value.ReviewID, Recovery: value.Recovery}
}

func safeIssues(xs []beads.Issue) []safeIssue {
	out := make([]safeIssue, len(xs))
	for i, x := range xs {
		out[i] = safeIssue{x.ID, x.Title, x.Status, x.Type, x.Labels, x.ReviewState}
	}
	return out
}
func safeRelease(value beads.ReleaseReadiness) safeReleaseResult {
	return safeReleaseResult{Ready: value.Ready, Blocking: safeIssues(value.Blocking)}
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
	fmt.Fprintln(out, "usage: smt new [FILE] | apply [--config FILE] PATH | init [PATH] | push [--dry-run] | worktree add PATH --branch NAME [--dry-run] | validate-message FILE | status [--json] | doctor | check --profile PROFILE | contracts validate | ci audit | ci contracts bump --id ID [--apply] | work ready [--json] | review | review list [--json] | review queue FEATURE --handoff PATH --evidence PATH [--json] | review requeue REVIEW [--json] | release check [--json]")
}
