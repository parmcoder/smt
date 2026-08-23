// Package apply builds the deliberately narrow, reviewed SMT blueprint.
package apply

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ggit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/runtime"
)

type Prerequisite interface{ Check(context.Context) error }
type Initializer interface {
	Initialize(context.Context, string) error
}
type commandPrerequisite func(context.Context) error

func (f commandPrerequisite) Check(ctx context.Context) error { return f(ctx) }

type commandInitializer func(context.Context, string) error

func (f commandInitializer) Initialize(ctx context.Context, path string) error { return f(ctx, path) }

var runBeads = func(ctx context.Context, dir, name string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

var runFlutterCreate = func(ctx context.Context, cwd string, args []string) error {
	cmd := exec.CommandContext(ctx, "asdf", args...)
	cmd.Dir = cwd
	_, err := cmd.CombinedOutput()
	return err
}

var runNextCreate = func(ctx context.Context, cwd string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "asdf", args...)
	cmd.Dir = cwd
	return cmd.CombinedOutput()
}

var writeStagedConfig = os.WriteFile

// Service stages every effect beside its target. Initialize is retained as a
// narrow failure seam; when absent, Config drives the built-in go-git builder.
type Service struct {
	Config        config.Config
	Prerequisites Prerequisite
	Initialize    Initializer
	Beads         Initializer
	Publish       func(string, string) error
	DefaultBeads  bool
}

func New() Service {
	return Service{Prerequisites: commandPrerequisite(checkTools), Publish: publish, DefaultBeads: true}
}

func checkTools(context.Context) error {
	for _, name := range []string{"codex", "asdf", "bd"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("required executable %q is unavailable", name)
		}
	}
	return nil
}

func initBeads(ctx context.Context, dir string) error {
	return initBeadsWithPrefix(ctx, dir, filepath.Base(dir))
}

func initBeadsWithPrefix(ctx context.Context, dir, prefix string) error {
	_, err := runBeads(ctx, dir, "bd", []string{"init", "--non-interactive", "--init-if-missing", "--quiet", "--skip-agents", "--skip-hooks", "--prefix", prefix})
	if err != nil {
		return fmt.Errorf("bd init failed: %w", err)
	}
	return nil
}

func publish(stage, destination string) error { return os.Rename(stage, destination) }

func (s Service) Apply(ctx context.Context, destination string, raw []byte) error {
	if err := config.LegacyDevOpsError(raw); err != nil {
		return err
	}
	if err := validateSelectableModuleMetadata(s.Config); err != nil {
		return err
	}
	if s.Prerequisites == nil || (s.Beads == nil && !s.DefaultBeads) {
		return fmt.Errorf("apply service dependencies are required")
	}
	if err := s.Prerequisites.Check(ctx); err != nil {
		return fmt.Errorf("apply prerequisites: %w", err)
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}
	parent := filepath.Dir(abs)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("destination parent is unavailable")
	}
	if _, err := os.Lstat(abs); err == nil {
		return fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination: %w", err)
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(abs)+".smt-")
	if err != nil {
		return fmt.Errorf("create stage: %w", err)
	}
	defer os.RemoveAll(stage)
	if err := writeStagedConfig(filepath.Join(stage, "smt.yaml"), raw, 0o600); err != nil {
		return fmt.Errorf("write staged config: %w", err)
	}
	if s.Initialize != nil {
		err = s.Initialize.Initialize(ctx, stage)
	} else {
		err = buildWorkspace(ctx, stage, abs, s.Config)
	}
	if err != nil {
		return fmt.Errorf("initialize staged workspace: %w", err)
	}
	agents, agentsErr := os.ReadFile(filepath.Join(stage, "AGENTS.md"))
	if agentsErr != nil && s.Initialize == nil {
		return fmt.Errorf("read staged AGENTS.md: %w", agentsErr)
	}
	if s.Beads != nil {
		err = s.Beads.Initialize(ctx, stage)
	} else {
		err = initBeadsWithPrefix(ctx, stage, filepath.Base(abs))
	}
	if err != nil {
		return fmt.Errorf("initialize staged beads: %w", err)
	}
	after, err := os.ReadFile(filepath.Join(stage, "AGENTS.md"))
	if agentsErr == nil && (err != nil || string(after) != string(agents)) {
		return fmt.Errorf("Beads init changed AGENTS.md")
	}
	if s.Initialize == nil {
		if err := commitRoot(stage, s.Config); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(abs); err == nil {
		return fmt.Errorf("destination already exists")
	}
	if s.Publish == nil {
		s.Publish = publish
	}
	if err := s.Publish(stage, abs); err != nil {
		return fmt.Errorf("publish staged workspace: %w", err)
	}
	return nil
}

func validateSelectableModuleMetadata(cfg config.Config) error {
	definitions := config.BuiltInModuleCatalog().Modules
	byModuleID := make(map[string]config.ModuleDefinition, len(definitions))
	for _, definition := range definitions {
		byModuleID[definition.ID] = definition
	}
	for _, repository := range cfg.Repositories {
		for _, moduleID := range repository.Modules {
			definition, ok := byModuleID[moduleID]
			if ok && !definition.Selectable {
				return fmt.Errorf("apply does not support non-selectable module %q", moduleID)
			}
		}
	}
	return nil
}

type component struct{ id, path, scope, kind, tech, title string }

func components(cfg config.Config) []component {
	byID := make(map[string]config.Repository, len(cfg.Repositories))
	for _, r := range cfg.Repositories[1:] {
		byID[r.ID] = r
	}
	var out []component
	for _, id := range []string{"web", "mobile", "api", "database"} {
		r, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, component{r.ID, r.Path, r.Scope, r.Component, r.Technology, r.Component + " component"})
	}
	return out
}

func buildWorkspace(ctx context.Context, root, publishedRoot string, cfg config.Config) error {
	if len(cfg.Repositories) == 0 {
		return fmt.Errorf("blueprint is required")
	}
	rootRepo, err := ggit.PlainInitWithOptions(root, &ggit.PlainInitOptions{
		InitOptions: ggit.InitOptions{DefaultBranch: plumbing.Main},
	})
	if err != nil {
		return err
	}
	cs := components(cfg)
	if err := writeArtifacts(root, publishedRoot, cfg, cs); err != nil {
		return err
	}
	databaseSelected := false
	for _, c := range cs {
		if c.id == "database" {
			databaseSelected = true
			break
		}
	}
	for _, c := range cs {
		if _, err := addChildWithDatabase(ctx, root, publishedRoot, c, databaseSelected); err != nil {
			return err
		}
	}
	_ = rootRepo
	return writeGitmodules(root, cs)
}

func addChild(ctx context.Context, root, publishedRoot string, c component) (plumbing.Hash, error) {
	return addChildWithDatabase(ctx, root, publishedRoot, c, false)
}

func addChildWithDatabase(ctx context.Context, root, publishedRoot string, c component, databaseSelected bool) (plumbing.Hash, error) {
	if err := ctx.Err(); err != nil {
		return plumbing.ZeroHash, err
	}
	bootstrap := filepath.Join(root, ".smt", "bootstrap", c.id)
	if c.id == "web" {
		if err := os.MkdirAll(filepath.Dir(bootstrap), 0o755); err != nil {
			return plumbing.ZeroHash, err
		}
		stagedWeb := filepath.Join(root, c.path)
		if err := os.MkdirAll(filepath.Dir(stagedWeb), 0o755); err != nil {
			return plumbing.ZeroHash, err
		}
		output, err := runNextCreate(ctx, root, []string{
			"exec",
			"npx",
			"--yes",
			"create-next-app@16.2.9",
			stagedWeb,
			"--typescript",
			"--eslint",
			"--app",
			"--empty",
			"--tailwind",
			"--use-npm",
			"--skip-install",
			"--disable-git",
			"--agents-md",
			"--import-alias=@/*",
		})
		if err != nil {
			return plumbing.ZeroHash, nextInitializationError(output, err)
		}
		if err := writeWebQualityFiles(stagedWeb); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("Web quality configuration failed: %w", err)
		}
		if err := writeWebRuntimeFiles(stagedWeb); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("Web runtime configuration failed: %w", err)
		}
		if err := removeNestedGitDirectories(stagedWeb); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("clean staged Web repository: %w", err)
		}
		if err := os.Rename(stagedWeb, bootstrap); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("stage Next.js Web repository: %w", err)
		}
	} else if c.id == "mobile" {
		if err := os.MkdirAll(filepath.Dir(bootstrap), 0o755); err != nil {
			return plumbing.ZeroHash, err
		}
		stagedMobile := filepath.Join(root, c.path)
		if err := os.MkdirAll(filepath.Dir(stagedMobile), 0o755); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := runFlutterCreate(ctx, root, []string{
			"exec",
			"flutter",
			"--suppress-analytics",
			"create",
			"--empty",
			"--no-pub",
			"--platforms=android,ios",
			"--org=com.example.smt",
			"--project-name=smt_mobile",
			"--description=A provider-neutral SMT Flutter mobile starter.",
			stagedMobile,
		}); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("Flutter mobile initialization failed; run `asdf install flutter 3.44.9-stable` and verify with `asdf current flutter`, then retry: %w", err)
		}
		if err := os.RemoveAll(filepath.Join(stagedMobile, ".idea")); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("remove staged Flutter IDE state: %w", err)
		}
		if err := writeMobileVerificationFiles(stagedMobile); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := os.Rename(stagedMobile, bootstrap); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("stage Flutter mobile repository: %w", err)
		}
	} else if err := os.MkdirAll(bootstrap, 0o755); err != nil {
		return plumbing.ZeroHash, err
	}
	repo, err := initChildRepository(bootstrap)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if c.id == "web" {
		if err := mergeIgnoreFile(filepath.Join(bootstrap, ".gitignore"), componentIgnore(c.kind)); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := writeFileIfAbsent(filepath.Join(bootstrap, "README.md"), componentReadme(c)); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := appendWebReadme(filepath.Join(bootstrap, "README.md")); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := writeLefthookConfigIfAbsent(bootstrap, filepath.Join(root, c.path), root); err != nil {
			return plumbing.ZeroHash, err
		}
	} else {
		if err := writeFile(filepath.Join(bootstrap, "README.md"), componentReadme(c)); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := writeFile(filepath.Join(bootstrap, ".gitignore"), componentIgnore(c.kind)); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := writeLefthookConfig(bootstrap, filepath.Join(root, c.path), root); err != nil {
			return plumbing.ZeroHash, err
		}
	}
	if c.id == "api" {
		if err := appendAPIReadme(filepath.Join(bootstrap, "README.md")); err != nil {
			return plumbing.ZeroHash, err
		}
		if err := writeAPISourceFiles(bootstrap, databaseSelected); err != nil {
			return plumbing.ZeroHash, err
		}
	}
	wt, err := repo.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if err := wt.AddWithOptions(&ggit.AddOptions{All: true}); err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := commit(repo, "chore("+c.scope+"): initialize "+c.id); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := copyDirectory(bootstrap, filepath.Join(root, c.path)); err != nil {
		return plumbing.ZeroHash, err
	}
	child, err := ggit.PlainOpen(filepath.Join(root, c.path))
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := child.CreateRemote(&gitconfig.RemoteConfig{Name: ggit.DefaultRemoteName, URLs: []string{filepath.Join(publishedRoot, ".smt", "bootstrap", c.id)}}); err != nil {
		return plumbing.ZeroHash, err
	}
	head, err := child.Head()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return head.Hash(), nil
}

func initChildRepository(path string) (*ggit.Repository, error) {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return ggit.PlainOpen(path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return ggit.PlainInitWithOptions(path, &ggit.PlainInitOptions{
		InitOptions: ggit.InitOptions{DefaultBranch: plumbing.Main},
	})
}

func nextInitializationError(output []byte, err error) error {
	message := fmt.Errorf("Next.js Web initialization failed; run `asdf install nodejs 24.18.0`, verify with `asdf current nodejs`, and inspect the pinned initializer with `asdf exec npx --yes create-next-app@16.2.9 --help`, then retry: %w", err)
	if len(output) == 0 {
		return message
	}
	return fmt.Errorf("%w; CLI output:\n%s", message, output)
}

func removeNestedGitDirectories(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root || entry.Name() != ".git" {
			return nil
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

func commitRoot(root string, cfg config.Config) error {
	repo, err := ggit.PlainOpen(root)
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := stageFiles(root, wt, components(cfg)); err != nil {
		return err
	}
	index, err := repo.Storer.Index()
	if err != nil {
		return err
	}
	for _, c := range components(cfg) {
		child, err := ggit.PlainOpen(filepath.Join(root, c.path))
		if err != nil {
			return err
		}
		h, err := child.Head()
		if err != nil {
			return err
		}
		e := index.Add(c.path)
		e.Hash = h.Hash()
		e.Mode = filemode.Submodule
	}
	if err := repo.Storer.SetIndex(index); err != nil {
		return err
	}
	submodules, err := wt.Submodules()
	if err != nil {
		return fmt.Errorf("read root submodules: %w", err)
	}
	if err := submodules.Init(); err != nil {
		return fmt.Errorf("initialize root submodules: %w", err)
	}
	_, err = commit(repo, "chore(repo): initialize workspace")
	return err
}

func stageFiles(root string, wt *ggit.Worktree, children []component) error {
	skip := map[string]bool{".git": true, ".smt": true}
	for _, c := range children {
		skip[c.path] = true
	}
	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("inspect generated files: %w", err)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		first := strings.Split(rel, string(filepath.Separator))[0]
		if skip[first] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			fileStatus, tracked := status[filepath.ToSlash(rel)]
			if !tracked || fileStatus.Worktree == ggit.Unmodified {
				return nil
			}
			if _, err := wt.Add(rel); err != nil {
				return fmt.Errorf("stage %s: %w", rel, err)
			}
		}
		return nil
	})
}

func commit(repo *ggit.Repository, message string) (plumbing.Hash, error) {
	wt, err := repo.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	return wt.Commit(message, &ggit.CommitOptions{Author: &object.Signature{Name: "SMT Bootstrap", Email: "smt@invalid"}})
}
func writeGitmodules(root string, cs []component) error {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "[submodule \"%s\"]\n\tpath = %s\n\turl = ./.smt/bootstrap/%s\n", c.id, c.path, c.id)
	}
	return writeFile(filepath.Join(root, ".gitmodules"), b.String())
}
func copyDirectory(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source must be a directory")
	}
	if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from, to := filepath.Join(source, e.Name()), filepath.Join(destination, e.Name())
		i, err := os.Lstat(from)
		if err != nil {
			return err
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink")
		}
		if i.IsDir() {
			if err := copyDirectory(from, to); err != nil {
				return err
			}
		} else if i.Mode().IsRegular() {
			data, err := os.ReadFile(from)
			if err != nil {
				return err
			}
			if err := os.WriteFile(to, data, i.Mode().Perm()); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("refuse non-regular file")
		}
	}
	return nil
}

func writeArtifacts(root, publishedRoot string, cfg config.Config, cs []component) error {
	selection := runtime.Selection{}
	for _, c := range cs {
		switch c.id {
		case runtime.ServiceWeb:
			selection.Web = true
		case runtime.ServiceAPI:
			selection.API = true
		case runtime.ServiceDatabase:
			selection.Database = true
		}
	}
	runtimeArtifacts, err := runtime.Render(runtime.RenderOptions{WorkspacePath: publishedRoot, Selection: selection})
	if err != nil {
		return fmt.Errorf("render runtime artifacts: %w", err)
	}
	files := map[string]string{
		".gitignore":                     "**/.DS_Store\n**/Thumbs.db\n**/desktop.ini\n\n.smt/\n.env\n",
		"compose.yaml":                   string(runtimeArtifacts.Compose),
		".env.example":                   string(runtimeArtifacts.EnvExample),
		"README.md":                      "# Platform workspace\n\nStart with [the documentation workspace](docs/README.md). Agents also read `AGENTS.md`.\n\nBeads configuration is tracked with the workspace; its embedded Dolt database and local runtime files stay on this machine and are ignored by Git.\n",
		".tool-versions":                 toolVersions(cs),
		"AGENTS.md":                      "# Project Agent Operating Agreement\n\nGo work uses `$godex:godex-go-backend`. Beads (`bd`) is the canonical task and issue state. Agents create tickets directly with `bd create`; the `work_manager` owns delivery decisions.\n\nWorkflow: `bd create -> worker -> tests -> manager review -> durable handoff/docs -> validation`. SMT does not wrap ticket creation, review queues, ready-work listing, or release readiness.\n\nOn the default branch, use ordinary `type(scope): summary` commits. On a Beads-ID branch, commits must use `type(scope): [BEAD-ID] summary`, with the ID exactly matching the branch.\n",
		"agents/work_manager.toml":       "name = \"work_manager\"\nmodel_reasoning_effort = \"high\"\n\n# Prepared workspace contract\n# Web delivery route: work_manager -> web_worker -> doc_writer.\ncommit_format = \"type(scope): [BEAD-ID] summary on a Beads-ID branch\"\n",
		"agents/integration_worker.toml": "name = \"integration_worker\"\nmodel = \"gpt-5.6-luna\"\nservice_tier = \"priority\"\nmodel_reasoning_effort = \"xhigh\"\n\n# Root-only integration contract; this is not a third delivery delegate.\nownership = \"root integration and gitlink updates only\"\ncommit_format = \"type(scope): [BEAD-ID] summary on a Beads-ID branch\"\n",
		"agents/doc_writer.toml":         "name = \"doc_writer\"\nmodel = \"gpt-5.6-luna\"\nservice_tier = \"priority\"\nmodel_reasoning_effort = \"xhigh\"\n",
		"prompts/build.md":               "# Build workflow\n\nCreate and manage tickets directly with Beads before editing code:\n\n`bd prime`\n`bd create --title=\"Short task title\" --description=\"Why this exists and what needs to be done\" --type=task --priority=2`\n`bd show <id>`\n`bd update <id> --claim`\n`bd ready`\n`bd blocked`\n`bd close <id> --reason=\"Completed\"`\n\nSMT does not wrap ticket creation, review queues, ready-work listing, or release readiness.\n\nOn the default branch use `type(scope): summary`; on a Beads-ID branch use `type(scope): [BEAD-ID] summary` with the ID exactly matching the branch.\n",
		"docs/README.md":                 "---\ntitle: Documentation Workspace\n---\n# Documentation Workspace\n\nUse [[00-project/Agentic Development Workflow]].\n",
		"docs/00-project/Agentic Development Workflow.md": "---\ntitle: Agentic Development Workflow\n---\n# Agentic Development Workflow\n\nBeads is canonical state. Agents create tickets directly with `bd create`, inspect and claim them with `bd show` and `bd update --claim`, and close them with `bd close`. Use `bd ready` and `bd blocked` to inspect work. SMT does not wrap ticket creation or review queues.\n",
	}
	webSelected := webE2ESelected(cfg, cs)
	mobileSelected := mobileE2ESelected(cfg, cs)
	if mobileSelected {
		for relative, contents := range mobileE2EFiles() {
			files[filepath.Join("e2e", "mobile", relative)] = contents
		}
	}
	if webSelected {
		for relative, contents := range webE2EFiles() {
			files[filepath.Join("e2e", "web", relative)] = contents
		}
	}
	if webSelected || mobileSelected {
		for relative, contents := range e2eOrchestrationFiles(webSelected, mobileSelected) {
			files[relative] = contents
		}
	}
	for _, c := range cs {
		if c.id == "web" {
			files["agents/web_worker.toml"] = webWorkerManifest()
			continue
		}
		files["agents/"+c.id+"_worker.toml"] = "name = \"" + c.id + "_worker\"\nmodel = \"gpt-5.6-luna\"\nservice_tier = \"priority\"\nmodel_reasoning_effort = \"xhigh\"\n\ncommit_format = \"type(scope): [BEAD-ID] summary on a matching Beads-ID branch\"\nprepared_workspace = \"Use the active branch Beads ID.\"\n"
	}
	for _, d := range []string{"docs/10-decisions", "docs/20-features", "docs/30-reviews", "docs/templates"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return err
		}
	}
	for p, v := range files {
		if err := writeFile(filepath.Join(root, p), v); err != nil {
			return err
		}
	}
	return writeLefthookConfig(root, root, root)
}

func mobileE2ESelected(cfg config.Config, cs []component) bool {
	if !e2eDeclared(cfg) {
		return false
	}
	for _, c := range cs {
		if c.id == "mobile" {
			return true
		}
	}
	return false
}

func webE2ESelected(cfg config.Config, cs []component) bool {
	if !e2eDeclared(cfg) {
		return false
	}
	for _, c := range cs {
		if c.id == "web" {
			return true
		}
	}
	return false
}

func e2eDeclared(cfg config.Config) bool {
	if len(cfg.Repositories) == 0 {
		return false
	}
	for _, module := range cfg.Repositories[0].Modules {
		if module == "e2e" {
			return true
		}
	}
	return false
}

func writeLefthookConfig(destination, repositoryRoot, workspaceRoot string) error {
	contents, err := lefthookConfig(repositoryRoot, workspaceRoot)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(destination, "lefthook.yml"), contents)
}

func lefthookConfig(repositoryRoot, workspaceRoot string) (string, error) {
	configPath, err := filepath.Rel(repositoryRoot, filepath.Join(workspaceRoot, "smt.yaml"))
	if err != nil {
		return "", fmt.Errorf("resolve root config path: %w", err)
	}
	return "no_auto_install: true\nassert_lefthook_installed: true\ncommit-msg:\n  commands:\n    validate-message:\n      run: smt validate-message --config " + filepath.ToSlash(configPath) + " {1}\n", nil
}

func writeFile(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value), 0o644)
}

func writeFileIfAbsent(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(value)
	return err
}

func appendWebReadme(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(contents)
	if !strings.Contains(text, "## SMT Web development") {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += `
## SMT Web development

Install dependencies and start the development server after Apply:

~~~sh
asdf exec npm ci
asdf exec npm run dev
~~~
`
	}
	if !strings.Contains(text, "## SMT Web quality") {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += `
## SMT Web quality

Run these explicit quality checks from this repository after installing
dependencies. Apply does not run a package manager; the committed lockfile is
the source of truth for installation.

~~~sh
asdf exec npm ci
asdf exec npm run format:check
asdf exec npm run lint
asdf exec npm run typecheck
asdf exec npm run test
asdf exec npm run build
~~~
`
	}
	if !strings.Contains(text, "## SMT Web runtime") {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += `
## SMT Web runtime

The generated Web runtime uses the pinned Node.js 24.18.0 toolchain and can
run directly or through the checked-in non-root Containerfile. The committed
package-lock.json is used for repeatable dependency installation:

~~~sh
asdf exec npm ci
asdf exec npm run build
asdf exec npm start
~~~

The production process uses the Next.js production server, listens on port
3000, and receives SIGTERM through the container's exec-form command so it
can shut down cleanly. The health contract is GET /healthz, which returns
HTTP 200 with status: ok. Set the optional server-only API_BASE_URL value
before npm start; it is validated without exposing its value to the browser.
The stable contract marker is data-smt-web-smoke="home".

Mobile is an Android/iOS workload and remains outside Compose; this Web child
does not install browsers, SDKs, credentials, or cloud integrations.
`
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func appendAPIReadme(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(contents)
	if strings.Contains(text, "## SMT API runtime") {
		return nil
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += `
## SMT API runtime

The generated API uses the pinned Go 1.26.5 toolchain. Install and select the
toolchain manually before running the child checks:

~~~sh
asdf install golang 1.26.5
asdf current golang
task format:check
task lint
task vuln
task test
task openapi
~~~

The pinned static checks are also available directly when Task is not
installed:

~~~sh
go tool golangci-lint run ./...
go tool govulncheck ./...
~~~

The API listens on HTTP_ADDR, defaulting to :8080. It exposes /healthz and
/readyz for health and readiness, and receives SIGTERM for graceful shutdown.
The generated Containerfile builds a static API binary and runs it as a
non-root UID/GID 10001 in the pinned Alpine 3.22 runtime. Podman is a
caller-owned prerequisite;
Apply does not install tools, build images, start containers, or execute tasks.

~~~sh
task container:build
task container:build:production
task container:verify
task verify
~~~

task container:build is the local development image path. It uses Podman's
--pull=missing policy to fetch an absent pinned base image once. Production
builds must use task container:build:production; that task uses
--pull=never, so the caller or build system must preload and verify the
pinned base images before building. Set SMT_API_PRODUCTION_IMAGE to choose the
production image tag; it defaults to smt-api:production.

If Go, the pinned Go tools, Task, or Podman is missing, record the lane as
unavailable, install or configure the prerequisite manually, and rerun the
command. This child contains only the API server and its local runtime checks;
other services and workspace coordination remain outside this slice. This work
is tracked by smt-4xf.3.3.4.
`
	return os.WriteFile(path, []byte(text), 0o644)
}

func writeLefthookConfigIfAbsent(destination, repositoryRoot, workspaceRoot string) error {
	contents, err := lefthookConfig(repositoryRoot, workspaceRoot)
	if err != nil {
		return err
	}
	return writeFileIfAbsent(filepath.Join(destination, "lefthook.yml"), contents)
}

func mergeIgnoreFile(path, required string) error {
	contents, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		contents = nil
	}
	merged := string(contents)
	for _, entry := range strings.Split(required, "\n") {
		if entry == "" || ignoreContains(merged, entry) {
			continue
		}
		if merged != "" && !strings.HasSuffix(merged, "\n") {
			merged += "\n"
		}
		merged += entry + "\n"
	}
	return writeFile(path, merged)
}

func ignoreContains(contents, want string) bool {
	for _, line := range strings.Split(contents, "\n") {
		got := strings.TrimSpace(line)
		if got == want || strings.TrimSuffix(got, "/") == strings.TrimSuffix(want, "/") {
			return true
		}
	}
	return false
}

func webWorkerManifest() string {
	return `name = "web_worker"
description = "GPT-5.6 Luna Next.js/TypeScript implementation worker for SMT Web starter generation and focused tests."
nickname_candidates = ["Web", "Canvas", "Browser"]
model = "gpt-5.6-luna"
service_tier = "priority"
model_reasoning_effort = "xhigh"
sandbox_mode = "workspace-write"
developer_instructions = """
Own only the assigned Next.js/TypeScript Web starter paths and focused tests
for the active Beads task. Use Next.js 16.2.9 on Node 24.18.0 with the
repository Web contract. Work test-first and keep generated Web output
provider-neutral. Required skills are
build-web-apps:react-best-practices and
build-web-apps:frontend-testing-debugging. Do not add package installation,
dependency resolution, secrets, MCP configuration, or domain CRUD to smt
apply. Do not delegate further. Return changed paths, checks and results,
assumptions, unresolved risks, and unverified behavior.
"""
commit_format = "type(scope): [BEAD-ID] summary on a matching Beads-ID branch"
prepared_workspace = "Use the active branch Beads ID."
`
}
func toolVersions(cs []component) string {
	v := []string{"task 3.52.0", "lefthook 2.1.10"}
	for _, c := range cs {
		switch c.id {
		case "api":
			v = append(v, "golang 1.26.5")
		case "web":
			v = append(v, "nodejs 24.18.0")
		case "mobile":
			v = append(v, "flutter 3.44.9-stable")
		}
	}
	return strings.Join(v, "\n") + "\n"
}
func componentIgnore(kind string) string {
	base := "**/.DS_Store\n**/Thumbs.db\n**/desktop.ini\n"
	switch kind {
	case "web":
		return base + "\nnode_modules/\n.next/\n.env\n"
	case "api":
		return base + "\nbin/\ntmp/\n.env\ncoverage.out\ncoverage.html\n"
	case "database":
		return base + "\npostgres-data/\n.env\n"
	case "mobile":
		return base + "\n.dart_tool/\nbuild/\n.idea/\n.flutter-plugins\n.flutter-plugins-dependencies\n.packages\nandroid/local.properties\nios/Flutter/Generated.xcconfig\nios/Flutter/flutter_export_environment.sh\n"
	default:
		return base + "\n.env\n"
	}
}

func componentReadme(c component) string {
	if c.kind == "mobile" {
		return `# Flutter mobile application

This is a deterministic SMT Flutter starter for Android and iOS. It runs
without an API; the optional SMT_API_BASE_URL value is reserved for
the later typed API integration.

## First run

Run these commands from this repository:

~~~sh
asdf install flutter 3.44.9-stable
asdf exec flutter pub get
asdf exec flutter doctor
asdf exec flutter devices
~~~

asdf install flutter 3.44.9-stable installs the version recorded by the
workspace .tool-versions file. If Flutter reports that the selected version
is not installed, run that command before continuing.

## Android emulator setup

Install Android Studio and its Android SDK, create an Android emulator, and
start it from Android Studio or the command below. A physical Android device
needs USB debugging enabled.

~~~sh
asdf exec flutter doctor --android-licenses
asdf exec flutter emulators
asdf exec flutter emulators --launch <emulator-id>
asdf exec flutter devices
asdf exec flutter run -d <device-id>
~~~

## iOS Simulator setup

Install the full Xcode application, accept its setup prompts, install
CocoaPods, and open the iOS Simulator before selecting a device.

~~~sh
brew install cocoapods
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -runFirstLaunch
asdf exec flutter pub get
open -a Simulator
asdf exec flutter devices
asdf exec flutter run -d <device-id>
~~~

Use asdf exec flutter build ios --debug --no-codesign for a local build check.
A real iPhone additionally requires Apple Developer signing configured in Xcode.

## Optional local API endpoint

The starter does not require Compose or an API. When a reachable API exists,
pass its base URL explicitly:

~~~sh
asdf exec flutter run -d <device-id> --dart-define=SMT_API_BASE_URL=http://127.0.0.1:8080
~~~

For an Android emulator, the host machine is usually 10.0.2.2; for a
physical phone, use the Mac's LAN address. The current starter only records
that the endpoint is configured; domain/API requests are a later milestone.
`
	}
	return "# " + c.title + "\n\nThis repository is a local SMT scaffold.\n"
}

// ValidateBlueprint accepts only the shape emitted by smt new. Remote URL
// changes are intentionally allowed after generation.
func ValidateBlueprint(cfg config.Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("apply requires an smt new blueprint")
	}
	if cfg.Provenance == nil {
		return fmt.Errorf("apply requires provenance")
	}
	if err := cfg.Provenance.Validate(); err != nil {
		return fmt.Errorf("apply requires %w", err)
	}
	if cfg.Workspace.AIAssist != "codex" || cfg.Workflow == nil {
		return fmt.Errorf("apply requires an smt new blueprint")
	}
	wantTypes := []string{"feat", "fix", "refactor", "perf", "test", "docs", "build", "ci", "chore", "revert"}
	if strings.Join(cfg.Commit.Types, ",") != strings.Join(wantTypes, ",") {
		return fmt.Errorf("apply requires the default commit types")
	}
	if cfg.Providers.GitLab.APIBaseURL != "" || cfg.Providers.GitLab.EnterpriseBaseURL != "" || cfg.Providers.GitLab.EnterpriseUploadURL != "" || cfg.Providers.GitHub.APIBaseURL != "" || cfg.Providers.GitHub.EnterpriseBaseURL != "" || cfg.Providers.GitHub.EnterpriseUploadURL != "" || len(cfg.Contracts.Reference)+len(cfg.Contracts.Artifact)+len(cfg.Contracts.MigrationCoverage) != 0 {
		return fmt.Errorf("apply requires only supported blueprint fields")
	}
	if err := validateSelectableModuleMetadata(cfg); err != nil {
		return err
	}
	if len(cfg.Repositories) < 2 || cfg.Repositories[0].ID != "repo" || cfg.Repositories[0].Path != "." || cfg.Repositories[0].Scope != "repo" || cfg.Repositories[0].Provider != "" || cfg.Repositories[0].Project != "" {
		return fmt.Errorf("apply requires the root blueprint repository")
	}
	if cfg.Repositories[0].HasChecks || len(cfg.Repositories[0].UnknownFields) != 0 {
		return fmt.Errorf("apply requires only supported blueprint fields")
	}
	qualityRoot, err := config.QualityRootModule(config.BuiltInModuleCatalog())
	if err != nil {
		return fmt.Errorf("apply requires a valid quality root module: %w", err)
	}
	if len(cfg.Repositories[0].Modules) > 1 || (len(cfg.Repositories[0].Modules) == 1 && cfg.Repositories[0].Modules[0] != qualityRoot.ID) {
		return fmt.Errorf("apply requires root modules to be omitted or exactly [%s]", qualityRoot.ID)
	}
	expected := []component{{"web", "web-app", "web", "web", "nextjs", ""}, {"mobile", "mobile-app", "mobile", "mobile", "flutter", ""}, {"api", "apis", "api", "api", "go", ""}, {"database", "database", "database", "database", "postgresql", ""}}
	stacks := []string{cfg.Workspace.Stack.Web, cfg.Workspace.Stack.Mobile, cfg.Workspace.Stack.API, cfg.Workspace.Stack.Database}
	scopes := []string{"repo"}
	n := 1
	for i, e := range expected {
		if stacks[i] == "" {
			continue
		}
		if i == 1 && stacks[i] != "flutter" {
			return fmt.Errorf("apply requires the fixed mobile stack")
		}
		if n >= len(cfg.Repositories) {
			return fmt.Errorf("apply repositories do not match selected stack")
		}
		r := cfg.Repositories[n]
		if r.ID != e.id || r.Path != e.path || r.Component != e.kind || r.Technology != e.tech || r.Scope != e.scope || r.Provider != "" || r.Project != "" || r.HasChecks || len(r.UnknownFields) != 0 {
			return fmt.Errorf("apply repositories do not match selected stack")
		}
		if len(r.Modules) != 1 || r.Modules[0] != e.id {
			return fmt.Errorf("apply repository modules do not match selected components")
		}
		scopes = append(scopes, e.scope)
		n++
	}
	if n != len(cfg.Repositories) || strings.Join(scopes, ",") != strings.Join(cfg.Commit.Scopes, ",") {
		if n != len(cfg.Repositories) {
			return fmt.Errorf("apply repository modules do not match selected components")
		}
		return fmt.Errorf("apply repositories do not match selected stack")
	}
	return nil
}
