package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
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
var doctorLookup = exec.LookPath
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
	command, err := root.ExecuteC()
	if err != nil {
		if code, ok := err.(commandExitError); ok {
			return int(code)
		}
		fmt.Fprintf(errOut, "error: %v\n", err)
		fmt.Fprint(errOut, command.UsageString())
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

func requireNonEmptyFlag(name string, value *string) cobra.PositionalArgs {
	return func(_ *cobra.Command, _ []string) error {
		if strings.TrimSpace(*value) == "" {
			return fmt.Errorf("required flag --%s must not be empty", name)
		}
		return nil
	}
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

	legacyLeaf := func(use, short, groupID string, path ...string) *cobra.Command {
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
	nativeLeaf := func(use, short, groupID, command string, args cobra.PositionalArgs, run func([]string, *logrus.Logger) int) *cobra.Command {
		return &cobra.Command{
			Use:     use,
			Short:   short,
			GroupID: groupID,
			Args:    args,
			RunE: func(_ *cobra.Command, args []string) error {
				return runNativeCommandWithVerbose(command, errOut, verbose, func(logger *logrus.Logger) int {
					return run(args, logger)
				})
			},
		}
	}

	newCommand := nativeLeaf("new [FILE]", "Create a workspace blueprint", "getting-started", "new", cobra.MaximumNArgs(1), func(args []string, _ *logrus.Logger) int {
		destination := "./smt.yaml"
		if len(args) == 1 {
			destination = args[0]
		}
		return runNew(destination, in, out, errOut)
	})
	var applyConfig string
	applyCommand := nativeLeaf("apply PATH", "Apply a workspace blueprint", "getting-started", "apply", cobra.ExactArgs(1), func(args []string, _ *logrus.Logger) int {
		return runApply(applyConfig, args[0], out, errOut)
	})
	applyCommand.Flags().StringVar(&applyConfig, "config", "./smt.yaml", "configuration file")

	var pushDryRun bool
	pushCommand := nativeLeaf("push", "Push configured repositories", "workspace", "push", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		cfg, _, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runPush(context.Background(), *cfg, git.ExecRunner{}, pushDryRun, out, errOut)
	})
	pushCommand.Flags().BoolVar(&pushDryRun, "dry-run", false, "validate and print the push plan")

	worktreeCommand := &cobra.Command{
		Use:     "worktree",
		Short:   "Manage linked worktrees",
		GroupID: "workspace",
		Long: `Create synchronized linked worktrees across the configured root and submodules.

Run smt worktree add PATH --branch NAME [--dry-run]. The branch must be new in every configured repository, and PATH must be outside the configured workspace. SMT completes every root and submodule preflight before creating anything, then creates the root worktree before nested child worktrees. Use --dry-run to inspect the plan without changing Git state. If a child creation fails after the root succeeds, use the reported paths for manual recovery; SMT does not remove worktrees automatically.`,
		Example: `  smt worktree add ../platform-feature --branch feature/demo --dry-run
  smt worktree add ../platform-feature --branch feature/demo`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	var worktreeBranch string
	var worktreeDryRun bool
	worktreeAddCommand := nativeLeaf("add PATH", "Create linked worktrees", "", "worktree", cobra.MatchAll(cobra.ExactArgs(1), requireNonEmptyFlag("branch", &worktreeBranch)), func(args []string, _ *logrus.Logger) int {
		cfg, root, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runWorktree(context.Background(), *cfg, root, git.ExecRunner{}, args[0], worktreeBranch, worktreeDryRun, out, errOut)
	})
	worktreeAddCommand.Flags().StringVar(&worktreeBranch, "branch", "", "new branch name")
	worktreeAddCommand.Flags().BoolVar(&worktreeDryRun, "dry-run", false, "validate and print the worktree plan")
	_ = worktreeAddCommand.MarkFlagRequired("branch")
	worktreeCommand.AddCommand(worktreeAddCommand)

	hooksCommand := &cobra.Command{
		Use:     "hooks",
		Short:   "Manage workspace Git hooks",
		GroupID: "workspace",
		Long:    "Install commit-msg hooks safely across the configured root and submodules. smt and lefthook must both be on PATH; from the SMT source checkout, run task build then export PATH=\"$PWD/bin:$PATH\". Return to the target workspace before installation.",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	var hooksDryRun bool
	hooksInstallCommand := nativeLeaf("install", "Install commit-msg hooks safely", "", "hooks install", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		cfg, root, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runHooksInstall(context.Background(), *cfg, root, hooksDryRun, out, errOut)
	})
	hooksInstallCommand.Flags().BoolVar(&hooksDryRun, "dry-run", false, "validate and print the hook install plan")
	hooksCommand.AddCommand(hooksInstallCommand)

	var statusJSON bool
	statusCommand := nativeLeaf("status", "Show workspace status", "workspace", "status", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		cfg, root, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runStatus(context.Background(), cfg, root, git.ExecRunner{}, statusJSON, out, errOut)
	})
	statusCommand.Flags().BoolVar(&statusJSON, "json", false, "write JSON output")
	statusCommand.Long = "Show Git state, commit-msg hook state, configured check profiles, and contract findings."

	doctorCommand := nativeLeaf("doctor", "Check local readiness", "workspace", "doctor", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		cfg, _, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runDoctor(context.Background(), cfg, git.ExecRunner{}, out, errOut)
	})
	doctorCommand.Long = "Check repository, hook, tool, and credential readiness with safe remediation."

	var validateConfig string
	validateCommand := nativeLeaf("validate-message FILE", "Validate a commit message", "developer-tools", "validate-message", cobra.ExactArgs(1), func(args []string, _ *logrus.Logger) int {
		return runValidateMessage(validateConfig, args[0], out, errOut)
	})
	validateCommand.Flags().StringVar(&validateConfig, "config", "./smt.yaml", "configuration file")
	var checkProfile, checkRepository string
	var checkAllowMutation, checkDryRun bool
	checkCommand := nativeLeaf("check", "Run a check profile", "developer-tools", "check", cobra.MatchAll(cobra.NoArgs, requireNonEmptyFlag("profile", &checkProfile)), func(_ []string, logger *logrus.Logger) int {
		cfg, _, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runCheck(context.Background(), *cfg, git.ExecRunner{}, checkProfile, checkRepository, checkAllowMutation, checkDryRun, out, errOut, logger)
	})
	checkCommand.Flags().StringVar(&checkProfile, "profile", "", "check profile")
	checkCommand.Flags().StringVar(&checkRepository, "repo", "", "repository ID")
	checkCommand.Flags().BoolVar(&checkAllowMutation, "allow-worktree-mutation", false, "allow checks that mutate the worktree")
	checkCommand.Flags().BoolVar(&checkDryRun, "dry-run", false, "validate without running checks")
	_ = checkCommand.MarkFlagRequired("profile")

	contractsCommand := legacyLeaf("contracts", "Inspect reusable contracts", "developer-tools", "contracts")
	contractsCommand.AddCommand(nativeLeaf("validate", "Validate contracts", "", "contracts", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		cfg, root, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runContractReport(*cfg, root, false, out, errOut)
	}))

	ciCommand := legacyLeaf("ci", "Run CI-parity tools", "developer-tools", "ci")
	ciContractsCommand := legacyLeaf("contracts", "Manage CI contracts", "", "ci", "contracts")
	ciAuditCommand := nativeLeaf("audit", "Audit CI parity", "", "ci", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		cfg, root, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runContractReport(*cfg, root, true, out, errOut)
	})
	var contractID string
	var contractApply bool
	ciContractsBumpCommand := nativeLeaf("bump", "Plan or apply a contract bump", "", "ci", cobra.MatchAll(cobra.NoArgs, requireNonEmptyFlag("id", &contractID)), func(_ []string, _ *logrus.Logger) int {
		cfg, root, code := loadConfig(errOut)
		if code != exitOK {
			return code
		}
		return runCIContractsBump(*cfg, root, contractID, contractApply, out, errOut)
	})
	ciContractsBumpCommand.Flags().StringVar(&contractID, "id", "", "reference contract ID")
	ciContractsBumpCommand.Flags().BoolVar(&contractApply, "apply", false, "apply the replacement")
	_ = ciContractsBumpCommand.MarkFlagRequired("id")
	ciContractsCommand.AddCommand(ciContractsBumpCommand)
	ciCommand.AddCommand(ciAuditCommand, ciContractsCommand)

	workCommand := &cobra.Command{
		Use:     "work",
		Short:   "Manage work items",
		GroupID: "review-workflow",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	var workReadyJSON bool
	workReadyCommand := nativeLeaf("ready", "List ready work", "", "work ready", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		return runWorkReady(context.Background(), ".", workReadyJSON, out, errOut)
	})
	workReadyCommand.Flags().BoolVar(&workReadyJSON, "json", false, "write JSON output")
	workCommand.AddCommand(workReadyCommand)

	reviewCommand := &cobra.Command{
		Use:     "review",
		Short:   "Open the review terminal interface",
		GroupID: "review-workflow",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runNativeCommandWithVerbose("review", errOut, verbose, func(_ *logrus.Logger) int {
				return runReview(in, out, errOut)
			})
		},
	}
	var reviewListJSON bool
	reviewListCommand := nativeLeaf("list", "List queued reviews", "", "review list", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		return runReviewList(context.Background(), ".", reviewListJSON, out, errOut)
	})
	reviewListCommand.Flags().BoolVar(&reviewListJSON, "json", false, "write JSON output")
	var reviewQueueHandoff, reviewQueueEvidence string
	var reviewQueueJSON bool
	reviewQueueCommand := nativeLeaf("queue FEATURE", "Queue a review", "", "review queue", cobra.MatchAll(cobra.ExactArgs(1), requireNonEmptyFlag("handoff", &reviewQueueHandoff), requireNonEmptyFlag("evidence", &reviewQueueEvidence)), func(args []string, _ *logrus.Logger) int {
		return runReviewQueue(context.Background(), ".", args[0], reviewQueueHandoff, reviewQueueEvidence, reviewQueueJSON, out, errOut)
	})
	reviewQueueCommand.Flags().StringVar(&reviewQueueHandoff, "handoff", "", "handoff path")
	reviewQueueCommand.Flags().StringVar(&reviewQueueEvidence, "evidence", "", "evidence path")
	reviewQueueCommand.Flags().BoolVar(&reviewQueueJSON, "json", false, "write JSON output")
	_ = reviewQueueCommand.MarkFlagRequired("handoff")
	_ = reviewQueueCommand.MarkFlagRequired("evidence")
	var reviewRequeueJSON bool
	reviewRequeueCommand := nativeLeaf("requeue REVIEW", "Requeue a review", "", "review requeue", cobra.ExactArgs(1), func(args []string, _ *logrus.Logger) int {
		return runReviewRequeue(context.Background(), ".", args[0], reviewRequeueJSON, out, errOut)
	})
	reviewRequeueCommand.Flags().BoolVar(&reviewRequeueJSON, "json", false, "write JSON output")
	reviewCommand.AddCommand(reviewListCommand, reviewQueueCommand, reviewRequeueCommand)

	releaseCommand := &cobra.Command{
		Use:     "release",
		Short:   "Check release readiness",
		GroupID: "review-workflow",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	var releaseCheckJSON bool
	releaseCheckCommand := nativeLeaf("check", "Check release readiness", "", "release check", cobra.NoArgs, func(_ []string, _ *logrus.Logger) int {
		return runReleaseCheck(context.Background(), ".", releaseCheckJSON, out, errOut)
	})
	releaseCheckCommand.Flags().BoolVar(&releaseCheckJSON, "json", false, "write JSON output")
	releaseCommand.AddCommand(releaseCheckCommand)
	root.AddCommand(newCommand, applyCommand, pushCommand, worktreeCommand, hooksCommand, statusCommand, doctorCommand, validateCommand, checkCommand, contractsCommand, ciCommand, workCommand, reviewCommand, releaseCommand)
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

func runNativeCommandWithVerbose(command string, errOut io.Writer, verbose bool, run func(*logrus.Logger) int) error {
	logger := newRunLogger(verbose, errOut)
	started := time.Now()
	code := run(logger)
	if verbose {
		logger.WithFields(logrus.Fields{
			"command":     command,
			"status":      commandStatus(code),
			"exit_code":   code,
			"duration_ms": time.Since(started).Milliseconds(),
		}).Debug("command finished")
	}
	if code != exitOK {
		return commandExitError(code)
	}
	return nil
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
	printUsage(errOut)
	return exitUsage
}

func runApply(configPath, destination string, out, errOut io.Writer) int {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(errOut, "apply: read config: %v\n", err)
		return exitValidation
	}
	cfg, err := config.LoadBytes(raw, configPath)
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
	if err := service.Apply(context.Background(), destination, raw); err != nil {
		fmt.Fprintf(errOut, "apply: %v\n", err)
		return exitValidation
	}
	fmt.Fprintln(out, "applied blueprint")
	return exitOK
}

func runNew(destination string, in io.Reader, out, errOut io.Writer) int {
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

func runPush(ctx context.Context, cfg config.Config, runner git.Runner, dryRun bool, out, errOut io.Writer) int {
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
	report, err := git.ExecutePush(ctx, runner, plan, dryRun)
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

func runWorktree(ctx context.Context, cfg config.Config, root string, runner git.Runner, destination, branch string, dryRun bool, out, errOut io.Writer) int {
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

func runHooksInstall(ctx context.Context, cfg config.Config, root string, dryRun bool, out, errOut io.Writer) int {
	runner := hooks.ExecRunner{}
	gitRunner := git.ExecRunner{}
	plan, err := hooks.PlanInstall(ctx, root, cfg.Repositories, exec.LookPath, git.Inspector{Runner: gitRunner}, gitRunner, runner)
	if err != nil {
		fmt.Fprintf(errOut, "hooks: %v\n", err)
		return exitValidation
	}
	if dryRun {
		for _, repository := range plan.Repositories {
			fmt.Fprintf(out, "hooks install plan: %s\n", repository.ID)
		}
	}
	report, err := hooks.ExecuteInstall(ctx, plan, runner, dryRun)
	if err != nil {
		fmt.Fprintf(errOut, "hooks: %v\ninstalled: %s\npending: %s\n", err, strings.Join(report.Installed, ","), strings.Join(report.Pending, ","))
		return exitValidation
	}
	if !dryRun {
		fmt.Fprintf(out, "hooks installed: %s\n", strings.Join(report.Installed, ","))
	}
	return exitOK
}

func runReview(in io.Reader, out, errOut io.Writer) int {
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

func writeAgentRoute(value any, jsonMode bool, out, errOut io.Writer) int {
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

func runWorkReady(ctx context.Context, root string, jsonMode bool, out, errOut io.Writer) int {
	xs, err := newBeadsService(root).ReadyWork(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "work ready: operation failed")
		return exitValidation
	}
	return writeAgentRoute(safeIssues(xs), jsonMode, out, errOut)
}

func runReviewList(ctx context.Context, root string, jsonMode bool, out, errOut io.Writer) int {
	xs, err := newBeadsService(root).ListReviews(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "review list: operation failed")
		return exitValidation
	}
	return writeAgentRoute(safeIssues(xs), jsonMode, out, errOut)
}

func runReviewQueue(ctx context.Context, root, featureID, handoff, evidence string, jsonMode bool, out, errOut io.Writer) int {
	result, err := newBeadsService(root).QueueReview(ctx, featureID, handoff, evidence)
	if err != nil {
		writeAgentRoute(safeQueue(result), jsonMode, out, errOut)
		fmt.Fprintln(errOut, "review queue: operation failed")
		return exitValidation
	}
	return writeAgentRoute(safeQueue(result), jsonMode, out, errOut)
}

func runReviewRequeue(ctx context.Context, root, reviewID string, jsonMode bool, out, errOut io.Writer) int {
	result, err := newBeadsService(root).RequeueAfterFix(ctx, reviewID)
	if err != nil {
		writeAgentRoute(safeRecovery{result.ReviewID, result.BugID, result.Recovery}, jsonMode, out, errOut)
		fmt.Fprintln(errOut, "review requeue: operation failed")
		return exitValidation
	}
	return writeAgentRoute(safeRecovery{result.ReviewID, result.BugID, result.Recovery}, jsonMode, out, errOut)
}

func runReleaseCheck(ctx context.Context, root string, jsonMode bool, out, errOut io.Writer) int {
	result, err := newBeadsService(root).ReleaseReadiness(ctx)
	if err != nil {
		fmt.Fprintln(errOut, "release check: operation failed")
		return exitValidation
	}
	if code := writeAgentRoute(safeRelease(result), jsonMode, out, errOut); code != exitOK {
		return code
	}
	if !result.Ready {
		return exitValidation
	}
	return exitOK
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

func runValidateMessage(configPath, path string, out, errOut io.Writer) int {
	message, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(errOut, "read message: %v\n", err)
		return exitInternal
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(errOut, "configuration error: %v\n", err)
		return exitInternal
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

func runStatus(ctx context.Context, cfg *config.Config, root string, runner git.Runner, jsonOutput bool, out, errOut io.Writer) int {
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
	if jsonOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(errOut, "encode status: %v\n", err)
			return exitInternal
		}
		return exitOK
	}
	renderStatus(out, result)
	return exitOK
}

func runDoctor(ctx context.Context, cfg *config.Config, runner git.Runner, out, errOut io.Writer) int {
	doctor := operations.NewDoctorWithHookInspector(*cfg, doctorLookup, func(name string) bool {
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
	renderDoctor(out, result)
	for _, check := range result.Checks {
		if check.Status == "error" {
			return exitValidation
		}
	}
	return exitOK
}

func renderStatus(out io.Writer, result statusOutput) {
	label := "OK"
	for _, entry := range result.Repositories {
		if !entry.Initialized || entry.Dirty || entry.Detached || entry.Error != "" || entry.HookStatus == hooks.HookAbsent || entry.HookStatus == hooks.HookUnmanaged {
			label = "WARN"
		}
	}
	if result.Contracts.Errors > 0 {
		label = "ERROR"
	}
	fmt.Fprintf(out, "STATUS: %s\n", label)
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "REPOSITORY\tPATH\tGIT\tBRANCH\tHOOK")
	for _, entry := range result.Repositories {
		gitState := "OK"
		if !entry.Initialized {
			gitState = "UNINITIALIZED"
		} else if entry.Detached {
			gitState = "DETACHED"
		} else if entry.Dirty {
			gitState = "DIRTY"
		}
		if entry.Error != "" {
			gitState = "ERROR"
		}
		branch := entry.Branch
		if branch == "" {
			branch = "-"
		}
		hook := string(entry.HookStatus)
		if hook == "" {
			hook = "unknown"
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", entry.ID, entry.Path, gitState, branch, hook)
	}
	_ = table.Flush()
	profiles := "none"
	if len(result.Profiles) > 0 {
		profiles = strings.Join(result.Profiles, ", ")
	}
	fmt.Fprintf(out, "profiles: %s\ncontracts: errors=%d warnings=%d\n", profiles, result.Contracts.Errors, result.Contracts.Warnings)
	steps := map[string]bool{}
	for _, entry := range result.Repositories {
		if entry.Dirty {
			steps["commit, stash, or discard local changes before workspace operations"] = true
		}
		if entry.Detached {
			steps["switch detached repositories to a branch before workspace operations"] = true
		}
		if !entry.Initialized || entry.Error != "" {
			steps["inspect the affected repository locally"] = true
		}
		if entry.HookStatus == hooks.HookAbsent {
			steps["run smt hooks install to install missing commit-msg hooks"] = true
		}
		if entry.HookStatus == hooks.HookUnmanaged {
			steps["custom commit-msg hooks are never overwritten; resolve them manually before smt hooks install"] = true
		}
	}
	if result.Contracts.Errors > 0 {
		steps["review contract errors"] = true
	} else if result.Contracts.Warnings > 0 {
		steps["review contract warnings"] = true
	}
	printSteps(out, steps)
}

func renderDoctor(out io.Writer, result operations.Result) {
	label := "OK"
	for _, check := range result.Checks {
		if check.Status == "error" {
			label = "ERROR"
			break
		}
		if check.Status == "warning" {
			label = "WARN"
		}
	}
	fmt.Fprintf(out, "DOCTOR: %s\n", label)
	missingSMT, missingLefthook := false, false
	for _, check := range result.Checks {
		if check.Status == "error" && check.ID == "tool:smt" {
			missingSMT = true
		}
		if check.Status == "error" && check.ID == "tool:lefthook" {
			missingLefthook = true
		}
	}
	groups := []struct {
		name  string
		match func(operations.Check) bool
	}{
		{"REPOSITORIES", func(check operations.Check) bool { return strings.HasPrefix(check.ID, "repo:") }},
		{"TOOLS", func(check operations.Check) bool { return check.ID == "git" || strings.HasPrefix(check.ID, "tool:") }},
		{"HOOKS", func(check operations.Check) bool { return strings.HasPrefix(check.ID, "hook:") }},
		{"CREDENTIALS", func(check operations.Check) bool { return strings.HasPrefix(check.ID, "token:") }},
	}
	steps := map[string]bool{}
	for _, group := range groups {
		var found bool
		for _, check := range result.Checks {
			if !group.match(check) {
				continue
			}
			if !found {
				fmt.Fprintln(out, group.name)
				found = true
			}
			message := check.Message
			if check.Status == "error" && strings.HasPrefix(check.ID, "hook:") {
				message = "hook inspection failed"
			}
			fmt.Fprintf(out, "%s %s - %s\n", checkLabel(check.Status), check.ID, message)
			if check.Status == "ok" {
				continue
			}
			switch {
			case strings.HasPrefix(check.ID, "hook:") && strings.Contains(check.Message, "absent") && !missingSMT && !missingLefthook:
				steps["run smt hooks install to install missing commit-msg hooks"] = true
			case strings.HasPrefix(check.ID, "hook:") && strings.Contains(check.Message, "unmanaged"):
				steps["custom commit-msg hooks are never overwritten; resolve them manually before smt hooks install"] = true
			case strings.HasPrefix(check.ID, "tool:lefthook"):
				steps["install lefthook and rerun smt doctor"] = true
			case check.ID == "tool:smt":
				steps["from the SMT source checkout, run task build then export PATH=\"$PWD/bin:$PATH\"; return to the target workspace and rerun smt doctor"] = true
			case strings.HasPrefix(check.ID, "token:"):
				steps["set SMT_"+strings.ToUpper(strings.TrimPrefix(check.ID, "token:"))+"_TOKEN before provider operations"] = true
			default:
				steps["inspect the affected repository locally"] = true
			}
		}
	}
	printSteps(out, steps)
}

func checkLabel(status string) string {
	if status == "warning" {
		return "WARN"
	}
	return strings.ToUpper(status)
}

func printSteps(out io.Writer, set map[string]bool) {
	if len(set) == 0 {
		return
	}
	steps := make([]string, 0, len(set))
	for step := range set {
		steps = append(steps, step)
	}
	sort.Strings(steps)
	fmt.Fprintln(out, "next steps:")
	for _, step := range steps {
		fmt.Fprintf(out, "- %s\n", step)
	}
}

func runCheck(ctx context.Context, cfg config.Config, runner git.Runner, profile, repoID string, allowMutation, dryRun bool, out, errOut io.Writer, logger *logrus.Logger) int {
	selected := cfg.Repositories
	if repoID != "" {
		selected = nil
		for _, repository := range cfg.Repositories {
			if repository.ID == repoID {
				selected = append(selected, repository)
			}
		}
		if selected == nil {
			fmt.Fprintf(errOut, "unknown repository %q\n", repoID)
			return exitUsage
		}
	}
	executor := commandExecutor{logger: logger.WithField("profile", profile)}
	for _, repository := range selected {
		if _, ok := repository.Profiles[profile]; !ok {
			fmt.Fprintf(errOut, "repository %q has no check profile %q\n", repository.ID, profile)
			return exitUsage
		}
		var changed []string
		if !dryRun {
			var err error
			changed, err = git.ChangedFiles(ctx, runner, git.Repository{ID: repository.ID, Dir: repository.Path})
			if err != nil {
				fmt.Fprintf(errOut, "check %s: %v\n", repository.ID, err)
				return exitInternal
			}
		}
		if err := checks.RunProfile(ctx, executor, repository, profile, changed, allowMutation, dryRun); err != nil {
			fmt.Fprintln(errOut, err)
			return exitValidation
		}
		fmt.Fprintf(out, "%s: %s checks passed\n", repository.ID, profile)
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

func runCIContractsBump(cfg config.Config, root, id string, apply bool, out, errOut io.Writer) int {
	service, err := newContracts(root, cfg)
	if err != nil {
		fmt.Fprintf(errOut, "contracts: %v\n", err)
		return exitInternal
	}
	plan, err := service.PlanBump(id)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitValidation
	}
	fmt.Fprintf(out, "contract %s (%s)\n--- before\n%s--- after\n%s", plan.ContractID, plan.Path, plan.Before, plan.After)
	if !apply {
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
	fmt.Fprintln(out, "usage: smt new [FILE] | apply [--config FILE] PATH | push [--dry-run] | worktree add PATH --branch NAME [--dry-run] | validate-message FILE | status [--json] | doctor | check --profile PROFILE | contracts validate | ci audit | ci contracts bump --id ID [--apply] | work ready [--json] | review | review list [--json] | review queue FEATURE --handoff PATH --evidence PATH [--json] | review requeue REVIEW [--json] | release check [--json]")
}
