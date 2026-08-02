// Package scaffold creates the local, Git-backed workspace produced by smt init.
package scaffold

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	ggit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/prereq"
	"gopkg.in/yaml.v3"
)

const (
	bootstrapName  = "SMT Bootstrap"
	bootstrapEmail = "smt@invalid"
)

// Selection is the fixed first-release platform profile chosen during init.
type Selection struct {
	Web      bool
	API      bool
	Database bool
	DevOps   bool
	Codex    bool
}

// Result describes the workspace created by Init.
type Result struct {
	Destination  string
	Repositories []string
}

// Prompt asks for the fixed first-release platform profile through a line-based
// terminal interface. Empty answers accept the displayed default.
func Prompt(in io.Reader, out io.Writer) (Selection, error) {
	reader := bufio.NewReader(in)
	web, err := askYesNo(reader, out, "Include Next.js web application? [Y/n] ", true)
	if err != nil {
		return Selection{}, err
	}
	api, err := askYesNo(reader, out, "Include Go API? [Y/n] ", true)
	if err != nil {
		return Selection{}, err
	}
	database, err := askYesNo(reader, out, "Include PostgreSQL database? [Y/n] ", true)
	if err != nil {
		return Selection{}, err
	}
	devops, err := askYesNo(reader, out, "Include Docker and OpenTofu DevOps? [Y/n] ", true)
	if err != nil {
		return Selection{}, err
	}
	return Selection{Web: web, API: api, Database: database, DevOps: devops, Codex: true}, nil
}

func askYesNo(reader *bufio.Reader, out io.Writer, prompt string, defaultValue bool) (bool, error) {
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return false, fmt.Errorf("write init prompt: %w", err)
	}
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read init prompt: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" && err == io.EOF {
		return false, fmt.Errorf("read init prompt: missing answer")
	}
	if answer == "" {
		return defaultValue, nil
	}
	switch answer {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("read init prompt: answer must be yes or no")
	}
}

// Prerequisites performs the setup gate and the later noninteractive Beads init.
type Prerequisites interface {
	Check(context.Context, prereq.Requirements) (prereq.Result, error)
	InitBeads(context.Context, string, string) error
}

// Service creates fresh local repositories through go-git.
type Service struct {
	prerequisites Prerequisites
	initRoot      func(string) (*ggit.Repository, error)
	writeRoot     func(string, Selection, []component) error
	afterBeads    func(context.Context, string) error
}

// New creates an initialization service with an explicit prerequisite adapter.
func New(prerequisites Prerequisites) *Service {
	return &Service{
		prerequisites: prerequisites,
		initRoot:      func(path string) (*ggit.Repository, error) { return ggit.PlainInit(path, false) },
		writeRoot:     writeRootFiles,
		afterBeads:    func(context.Context, string) error { return nil },
	}
}

// Init creates an empty root repository and selected local bootstrap submodules.
func (s *Service) Init(ctx context.Context, destination string, selection Selection) (Result, error) {
	if s == nil {
		return Result{}, fmt.Errorf("initialize workspace: service is required")
	}
	components := selectedComponents(selection)
	if len(components) == 0 {
		return Result{}, fmt.Errorf("initialize workspace: select at least one component")
	}
	if s.prerequisites == nil {
		return Result{}, fmt.Errorf("initialize workspace: prerequisites are required")
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return Result{}, fmt.Errorf("initialize workspace: resolve destination: %w", err)
	}
	if err := ensureNewDestination(destination); err != nil {
		return Result{}, err
	}
	prefix, err := beadsPrefix(destination)
	if err != nil {
		return Result{}, err
	}
	readiness, err := s.prerequisites.Check(ctx, Requirements(selection))
	if err != nil {
		return Result{}, fmt.Errorf("initialize workspace: check prerequisites: %w", err)
	}
	if !readiness.Ready() {
		return Result{}, readinessError(readiness)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return Result{}, fmt.Errorf("initialize workspace: create destination parent: %w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".smt-")
	if err != nil {
		return Result{}, fmt.Errorf("initialize workspace: create staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := s.initialize(ctx, staging, destination, prefix, selection, components); err != nil {
		return Result{}, err
	}
	if err := os.Rename(staging, destination); err != nil {
		return Result{}, fmt.Errorf("initialize workspace: publish destination: %w", err)
	}
	result := Result{Destination: destination, Repositories: make([]string, 0, len(components)+1)}
	result.Repositories = append(result.Repositories, "repo")
	for _, component := range components {
		result.Repositories = append(result.Repositories, component.ID)
	}
	return result, nil
}

func repointBootstrapRemotes(root, publishedRoot string, components []component) error {
	for _, component := range components {
		repository, err := ggit.PlainOpen(filepath.Join(root, component.Path))
		if err != nil {
			return fmt.Errorf("initialize workspace: reopen %s after publish: %w", component.ID, err)
		}
		if err := repository.DeleteRemote(ggit.DefaultRemoteName); err != nil {
			return fmt.Errorf("initialize workspace: replace %s origin: %w", component.ID, err)
		}
		if _, err := repository.CreateRemote(&gitconfig.RemoteConfig{Name: ggit.DefaultRemoteName, URLs: []string{filepath.Join(publishedRoot, ".smt", "bootstrap", component.ID)}}); err != nil {
			return fmt.Errorf("initialize workspace: configure %s origin: %w", component.ID, err)
		}
	}
	return nil
}

func (s *Service) initialize(ctx context.Context, destination, publishedDestination, prefix string, selection Selection, components []component) error {
	if s.initRoot == nil || s.writeRoot == nil || s.afterBeads == nil {
		return fmt.Errorf("initialize workspace: service is incomplete")
	}
	rootRepository, err := s.initRoot(destination)
	if err != nil {
		return fmt.Errorf("initialize workspace: initialize root repository: %w", err)
	}
	if err := s.writeRoot(destination, selection, components); err != nil {
		return err
	}
	agentsPath := filepath.Join(destination, "AGENTS.md")
	agentsBefore, err := os.ReadFile(agentsPath)
	if err != nil {
		return fmt.Errorf("initialize workspace: read AGENTS.md before Beads init: %w", err)
	}
	if err := s.prerequisites.InitBeads(ctx, destination, prefix); err != nil {
		return fmt.Errorf("initialize workspace: initialize Beads: %w", err)
	}
	agentsAfter, err := os.ReadFile(agentsPath)
	if err != nil {
		return fmt.Errorf("initialize workspace: verify AGENTS.md after Beads init: %w", err)
	}
	if string(agentsAfter) != string(agentsBefore) {
		return fmt.Errorf("initialize workspace: Beads init changed AGENTS.md")
	}
	if err := s.afterBeads(ctx, destination); err != nil {
		return fmt.Errorf("initialize workspace: post-Beads validation: %w", err)
	}
	childHeads := make(map[string]plumbing.Hash, len(components))
	for _, component := range components {
		head, err := s.addBootstrapSubmodule(ctx, destination, component)
		if err != nil {
			return err
		}
		childHeads[component.ID] = head
	}
	if err := writeGitmodules(destination, components); err != nil {
		return err
	}
	if err := stageRoot(rootRepository, selection, components, childHeads); err != nil {
		return err
	}
	if _, err := commit(rootRepository, "chore(repo): initialize workspace"); err != nil {
		return err
	}
	if err := repointBootstrapRemotes(destination, publishedDestination, components); err != nil {
		return err
	}
	return nil
}

func (s *Service) addBootstrapSubmodule(ctx context.Context, root string, component component) (plumbing.Hash, error) {
	if err := ctx.Err(); err != nil {
		return plumbing.ZeroHash, err
	}
	if !validComponent(component) {
		return plumbing.ZeroHash, fmt.Errorf("initialize workspace: invalid fixed component %q", component.ID)
	}
	bootstrap := filepath.Join(root, ".smt", "bootstrap", component.ID)
	if err := os.MkdirAll(bootstrap, 0o755); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s bootstrap: %w", component.ID, err)
	}
	child, err := ggit.PlainInit(bootstrap, false)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s bootstrap repository: %w", component.ID, err)
	}
	if err := writeComponentFiles(bootstrap, component); err != nil {
		return plumbing.ZeroHash, err
	}
	worktree, err := child.Worktree()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s bootstrap worktree: %w", component.ID, err)
	}
	if err := worktree.AddWithOptions(&ggit.AddOptions{All: true}); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s bootstrap stage: %w", component.ID, err)
	}
	if _, err := commit(child, "chore("+component.Scope+"): initialize "+component.ID); err != nil {
		return plumbing.ZeroHash, err
	}
	componentPath := filepath.Join(root, component.Path)
	if err := copyBootstrap(bootstrap, componentPath); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s component checkout: %w", component.ID, err)
	}
	cloned, err := ggit.PlainOpen(componentPath)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s component open: %w", component.ID, err)
	}
	if _, err := cloned.CreateRemote(&gitconfig.RemoteConfig{Name: ggit.DefaultRemoteName, URLs: []string{bootstrap}}); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s component origin: %w", component.ID, err)
	}
	head, err := cloned.Head()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("initialize %s component head: %w", component.ID, err)
	}
	return head.Hash(), nil
}

// copyBootstrap creates the one fixed local child checkout used by a newly
// scaffolded workspace. It is deliberately not a general clone implementation.
func copyBootstrap(source, destination string) error {
	if filepath.IsAbs(destination) == false {
		return fmt.Errorf("destination must be absolute")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("destination already exists")
		}
		return err
	}
	return copyDirectory(source, destination)
}

func copyDirectory(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source must be a non-symlink directory")
	}
	if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "" || entry.Name() == "." || entry.Name() == ".." {
			return fmt.Errorf("invalid source entry")
		}
		sourcePath, destinationPath := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
		entryInfo, err := os.Lstat(sourcePath)
		if err != nil {
			return err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink %s", entry.Name())
		}
		if entryInfo.IsDir() {
			if err := copyDirectory(sourcePath, destinationPath); err != nil {
				return err
			}
			continue
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("refuse non-regular file %s", entry.Name())
		}
		contents, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destinationPath, contents, entryInfo.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func commit(repository *ggit.Repository, message string) (plumbing.Hash, error) {
	worktree, err := repository.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return worktree.Commit(message, &ggit.CommitOptions{Author: &object.Signature{Name: bootstrapName, Email: bootstrapEmail}})
}

func writeGitmodules(root string, components []component) error {
	var builder strings.Builder
	for _, component := range components {
		if !validComponent(component) {
			return fmt.Errorf("initialize workspace: invalid fixed component %q", component.ID)
		}
		fmt.Fprintf(&builder, "[submodule \"%s\"]\n\tpath = %s\n\turl = ./.smt/bootstrap/%s\n", component.ID, component.Path, component.ID)
	}
	return writeFile(filepath.Join(root, ".gitmodules"), builder.String())
}

func stageRoot(repository *ggit.Repository, selection Selection, components []component, heads map[string]plumbing.Hash) error {
	worktree, err := repository.Worktree()
	if err != nil {
		return fmt.Errorf("initialize workspace: root worktree: %w", err)
	}
	paths := []string{".gitignore", "README.md", "smt.yaml", ".gitmodules", ".tool-versions", "AGENTS.md", "agents", "prompts", "docs", ".beads/.gitignore", ".beads/README.md", ".beads/config.yaml", ".beads/metadata.json", ".beads/interactions.jsonl"}
	for _, path := range paths {
		if _, err := worktree.Add(path); err != nil {
			return fmt.Errorf("initialize workspace: stage %s: %w", path, err)
		}
	}
	index, err := repository.Storer.Index()
	if err != nil {
		return fmt.Errorf("initialize workspace: read root index: %w", err)
	}
	for _, component := range components {
		head, ok := heads[component.ID]
		if !ok || head.IsZero() {
			return fmt.Errorf("initialize workspace: missing %s child head", component.ID)
		}
		if existing, _ := index.Entry(component.Path); existing != nil {
			_, _ = index.Remove(component.Path)
		}
		entry := index.Add(component.Path)
		entry.Hash = head
		entry.Mode = filemode.Submodule
	}
	if err := repository.Storer.SetIndex(index); err != nil {
		return fmt.Errorf("initialize workspace: write root gitlinks: %w", err)
	}
	return nil
}

func validComponent(component component) bool {
	return (component.ID == "web" && component.Path == "web-app") || (component.ID == "api" && component.Path == "apis") || (component.ID == "database" && component.Path == "database") || (component.ID == "infra" && component.Path == "devops")
}

func ensureNewDestination(destination string) error {
	if _, err := os.Lstat(destination); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("initialize workspace: inspect destination: %w", err)
	}
	return fmt.Errorf("initialize workspace: destination %s already exists", destination)
}

var beadsPrefixPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func beadsPrefix(destination string) (string, error) {
	prefix := filepath.Base(destination)
	if !beadsPrefixPattern.MatchString(prefix) {
		return "", fmt.Errorf("initialize workspace: destination basename %q is not a valid Beads prefix", prefix)
	}
	return prefix, nil
}

type component struct {
	ID         string
	Path       string
	Scope      string
	Kind       string
	Technology string
	Title      string
}

func selectedComponents(selection Selection) []component {
	components := make([]component, 0, 4)
	if selection.Web {
		components = append(components, component{ID: "web", Path: "web-app", Scope: "web", Kind: "web", Technology: "nextjs", Title: "Next.js web application"})
	}
	if selection.API {
		components = append(components, component{ID: "api", Path: "apis", Scope: "api", Kind: "api", Technology: "go", Title: "Go API"})
	}
	if selection.Database {
		components = append(components, component{ID: "database", Path: "database", Scope: "database", Kind: "database", Technology: "postgresql", Title: "PostgreSQL database"})
	}
	if selection.DevOps {
		components = append(components, component{ID: "infra", Path: "devops", Scope: "infra", Kind: "devops", Technology: "docker-opentofu", Title: "Docker and OpenTofu DevOps"})
	}
	return components
}

// Requirements is the single prerequisite contract used by setup checks and
// the atomic initializer.
func Requirements(selection Selection) prereq.Requirements {
	requirements := prereq.Requirements{Plugins: []prereq.Plugin{
		{Source: "parmcoder/codex-obsidian", Selector: "codex-obsidian@codex-obsidian"},
		{Source: "parmcoder/godex", Selector: "godex@godex"},
	}, Runtimes: []prereq.Runtime{{Plugin: "task", Version: "3.52.0"}, {Plugin: "lefthook", Version: "2.1.10"}}}
	if selection.API {
		requirements.Runtimes = append(requirements.Runtimes, prereq.Runtime{Plugin: "golang", Version: "1.26.5"})
	}
	if selection.Web {
		requirements.Runtimes = append(requirements.Runtimes, prereq.Runtime{Plugin: "nodejs", Version: "24.18.0"})
	}
	if selection.DevOps {
		requirements.Runtimes = append(requirements.Runtimes, prereq.Runtime{Plugin: "opentofu", Version: "1.12.3"})
	}
	return requirements
}

func readinessError(result prereq.Result) error {
	parts := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if finding.Status == prereq.StatusReady {
			continue
		}
		part := finding.Message
		if finding.Guidance != "" {
			part += ": " + finding.Guidance
		}
		parts = append(parts, part)
	}
	return fmt.Errorf("initialize workspace: prerequisites are not ready: %s", strings.Join(parts, "; "))
}

func toolVersions(selection Selection) string {
	lines := []string{"task 3.52.0", "lefthook 2.1.10"}
	if selection.API {
		lines = append(lines, "golang 1.26.5")
	}
	if selection.Web {
		lines = append(lines, "nodejs 24.18.0")
	}
	if selection.DevOps {
		lines = append(lines, "opentofu 1.12.3")
	}
	return strings.Join(lines, "\n") + "\n"
}

func writeRootFiles(root string, selection Selection, components []component) error {
	if err := writeFile(filepath.Join(root, ".gitignore"), rootIgnore); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "README.md"), "# Platform workspace\n\nStart with [the documentation workspace](docs/README.md), [the agentic workflow](docs/00-project/Agentic%20Development%20Workflow.md), and [the human review queue](docs/Review%20Queue.base). Agents also read `AGENTS.md`.\n"); err != nil {
		return err
	}
	if err := writeConfig(filepath.Join(root, "smt.yaml"), selection, components); err != nil {
		return err
	}
	if err := writeCodexFiles(root, components); err != nil {
		return err
	}
	if err := writeDocsWorkspace(root); err != nil {
		return err
	}
	return writeFile(filepath.Join(root, ".tool-versions"), toolVersions(selection))
}

func writeConfig(path string, selection Selection, components []component) error {
	stack := config.WorkspaceStack{}
	if selection.Web {
		stack.Web = "nextjs"
	}
	if selection.API {
		stack.API = "go"
	}
	if selection.Database {
		stack.Database = "postgresql"
	}
	if selection.DevOps {
		stack.DevOps = []string{"docker", "opentofu"}
	}
	repositories := []config.Repository{{ID: "repo", Path: ".", Scope: "repo", Remote: config.Remote{}}}
	scopes := []string{"repo"}
	for _, component := range components {
		repositories = append(repositories, config.Repository{
			ID: component.ID, Path: component.Path, Component: component.Kind,
			Technology: component.Technology, Scope: component.Scope, Remote: config.Remote{},
		})
		scopes = append(scopes, component.Scope)
	}
	generated := struct {
		Version      int                 `yaml:"version"`
		Workspace    config.Workspace    `yaml:"workspace"`
		Workflow     *config.Workflow    `yaml:"workflow"`
		Commit       config.CommitConfig `yaml:"commit"`
		Repositories []config.Repository `yaml:"repositories"`
	}{
		Version:   1,
		Workspace: config.Workspace{Stack: stack},
		Workflow: &config.Workflow{IssueTracker: "beads", DocsPath: "docs", ReviewPolicy: "release-gate", RequiredPlugins: []config.RequiredPlugin{
			{Source: "parmcoder/codex-obsidian", Selector: "codex-obsidian@codex-obsidian"},
			{Source: "parmcoder/godex", Selector: "godex@godex"},
		}},
		Commit:       config.CommitConfig{Types: []string{"feat", "fix", "refactor", "perf", "test", "docs", "build", "ci", "chore", "revert"}, Scopes: scopes},
		Repositories: repositories,
	}
	generated.Workspace.AIAssist = "codex"
	data, err := yaml.Marshal(generated)
	if err != nil {
		return fmt.Errorf("initialize workspace: encode config: %w", err)
	}
	return writeFile(path, string(data))
}

func writeComponentFiles(root string, component component) error {
	readme := "# " + component.Title + "\n\nThis repository is a local SMT scaffold. Build the implementation through the generated Codex workflow.\n"
	if err := writeFile(filepath.Join(root, "README.md"), readme); err != nil {
		return err
	}
	return writeFile(filepath.Join(root, ".gitignore"), componentIgnore(component.Kind))
}

func writeCodexFiles(root string, components []component) error {
	workers := make([]string, 0, len(components))
	for _, component := range components {
		workers = append(workers, component.ID+"_worker")
		if err := writeFile(filepath.Join(root, "agents", component.ID+"_worker.toml"), workerManifest(component)); err != nil {
			return err
		}
	}
	agreement := "# Project Agent Operating Agreement\n\nGo work uses `$godex:godex-go-backend`. Durable documentation uses the installed Codex Obsidian skills. Beads (`bd`) is the canonical task and issue state; do not create a second issue tracker in Markdown. The `work_manager` owns delivery decisions and assigns one component worker at a time.\n\nEach completed feature must record changed paths, checks and results, assumptions, risks, unverified behavior, and executable E2E evidence. The manager reviews implementation first, then durable handoff/docs are written, then a human owns E2E review. On pass the human closes the review and then the feature may close; on fail the human creates a linked bug and the same review is re-tested. Open reviews or bugs block release. Agents must never approve or close human reviews.\n\nWorkflow: `work_manager -> component worker -> tests -> manager review -> durable handoff/docs -> human E2E review -> release gate`.\n\nActive workers: " + strings.Join(workers, ", ") + ".\n"
	if err := writeFile(filepath.Join(root, "AGENTS.md"), agreement); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "agents", "work_manager.toml"), workManagerManifest); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "agents", "doc_writer.toml"), docWriterManifest); err != nil {
		return err
	}
	return writeFile(filepath.Join(root, "prompts", "build.md"), "# Build workflow\n\n1. Read `AGENTS.md`, `smt.yaml`, and [[../docs/00-project/Agentic Development Workflow]].\n2. Use `bd` for canonical task state; the work manager writes one decision-complete assignment.\n3. Manager review precedes durable handoff/docs with changed paths, checks, assumptions, risks, unverified behavior, and E2E evidence.\n4. Queue a human E2E review. Pass closes the review then the feature; fail creates a linked bug and re-queues the same review for retest. Open reviews and bugs block release. Agents never close human reviews.\n")
}

func writeDocsWorkspace(root string) error {
	files := map[string]string{
		"docs/README.md": `---
title: Documentation Workspace
---
# Documentation Workspace

Use [[00-project/Agentic Development Workflow]] for the review loop. Beads is the canonical issue state; [[Review Queue.base]] is view-only evidence, not a second issue tracker.
`,
		"docs/00-project/Agentic Development Workflow.md": `---
title: Agentic Development Workflow
---
# Agentic Development Workflow

Beads is the canonical issue state; this workspace records durable evidence only. The manager reviews implementation, then agents record changed paths, checks, assumptions, risks, unverified behavior, and E2E evidence in [[../templates/Feature Handoff]]. Humans own [[../templates/Human E2E Review]] pass/fail decisions. Pass closes the review and then the feature; failure creates a linked [[../templates/Bug Report]] and re-queues the same review for retest. Open reviews or bugs block release. Agents must never close human reviews.

~~~mermaid
flowchart LR
Feature --> Review[Human E2E review]
Review -->|pass| Release[Release gate]
Review -->|fail| Bug[Bug ticket]
Bug --> Retest[Same review re-queued]
Retest --> Review
~~~
`,
		"docs/templates/Feature Handoff.md": `---
title: Feature Handoff
type: feature-handoff
status: queued
feature_issue: ""
review_issue: ""
---
# Feature Handoff

Links: [[../00-project/Agentic Development Workflow]]

## Changed paths
## Checks and results
## Assumptions
## Risks
## Unverified behavior
## Human E2E evidence and instructions
`,
		"docs/templates/Human E2E Review.md": `---
title: Human E2E Review
type: human-e2e-review
status: queued
feature_issue: ""
review_issue: ""
bug_issue: ""
reviewer: ""
evidence: ""
updated: ""
---
# Human E2E Review

Links: [[Feature Handoff]] and [[Bug Report]]

Status is queued, passed, failed, or retest-queued. Record pass/fail, reviewer, executable evidence, and retest outcome. Pass closes this review then the feature; fail creates a linked bug and re-queues this same review. Agents must not close this review.
`,
		"docs/templates/Bug Report.md": `---
title: Bug Report
type: bug-report
status: open
bug_issue: ""
feature_issue: ""
discovered_from_review_issue: ""
---
# Bug Report

Links: [[Human E2E Review]]

## Title
## Reproduction
## Expected behavior
## Actual behavior
## Evidence
`,
		"docs/Review Queue.base": `# View-only human-review evidence; Beads remains canonical state.
filters:
  and:
    - file.inFolder("30-reviews")
views:
  - type: table
    name: Human E2E Reviews
`,
	}
	for _, directory := range []string{"docs/10-decisions", "docs/20-features", "docs/30-reviews"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			return fmt.Errorf("initialize workspace: create docs directory: %w", err)
		}
	}
	for path, contents := range files {
		if err := writeFile(filepath.Join(root, path), contents); err != nil {
			return err
		}
	}
	return nil
}

func workerManifest(component component) string {
	return "name = \"" + component.ID + "_worker\"\n" +
		"description = \"Codex worker for the " + component.Title + " repository.\"\n" +
		"model_reasoning_effort = \"medium\"\n" +
		"developer_instructions = \"Implement only manager-assigned work in " + component.Path + ", add focused tests, and report changed paths, checks, risks, and unverified behavior.\"\n"
}

func writeFile(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("initialize workspace: create parent directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("initialize workspace: write %s: %w", path, err)
	}
	return nil
}

func componentIgnore(kind string) string {
	base := metadataIgnore
	switch kind {
	case "web":
		return base + "\nnode_modules/\n.next/\ncoverage/\n.env\n.env.*\n!.env.example\n"
	case "api":
		return base + "\nbin/\ntmp/\n.env\n.env.*\n!.env.example\n"
	case "database":
		return base + "\npostgres-data/\n.env\n.env.*\n!.env.example\n"
	default:
		return base + "\n.terraform/\n.tofu/\n*.tfstate\n*.tfstate.*\n.env\n.env.*\n!.env.example\n"
	}
}

const metadataIgnore = "**/.DS_Store\n**/Thumbs.db\n**/desktop.ini\n"

const rootIgnore = metadataIgnore + "\n.smt/\n"

const workManagerManifest = "name = \"work_manager\"\ndescription = \"Codex delivery controller for this workspace.\"\nmodel_reasoning_effort = \"high\"\ndeveloper_instructions = \"Assign one decision-complete component task at a time, review tests and diffs, and never implement worker-owned production code.\"\n"

const docWriterManifest = "name = \"doc_writer\"\ndescription = \"Codex documentation worker for this workspace.\"\nmodel_reasoning_effort = \"low\"\ndeveloper_instructions = \"Update documentation and prompts after accepted behavior without changing component runtime behavior.\"\n"
