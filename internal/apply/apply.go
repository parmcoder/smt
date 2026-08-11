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

type component struct{ id, path, scope, kind, tech, title string }

func components(cfg config.Config) []component {
	var out []component
	for _, r := range cfg.Repositories[1:] {
		out = append(out, component{r.ID, r.Path, r.Scope, r.Component, r.Technology, r.Component + " component"})
	}
	return out
}

func buildWorkspace(ctx context.Context, root, publishedRoot string, cfg config.Config) error {
	if len(cfg.Repositories) == 0 {
		return fmt.Errorf("blueprint is required")
	}
	rootRepo, err := ggit.PlainInit(root, false)
	if err != nil {
		return err
	}
	cs := components(cfg)
	if err := writeArtifacts(root, cs); err != nil {
		return err
	}
	for _, c := range cs {
		if _, err := addChild(ctx, root, publishedRoot, c); err != nil {
			return err
		}
	}
	_ = rootRepo
	return writeGitmodules(root, cs)
}

func addChild(ctx context.Context, root, publishedRoot string, c component) (plumbing.Hash, error) {
	if err := ctx.Err(); err != nil {
		return plumbing.ZeroHash, err
	}
	bootstrap := filepath.Join(root, ".smt", "bootstrap", c.id)
	if err := os.MkdirAll(bootstrap, 0o755); err != nil {
		return plumbing.ZeroHash, err
	}
	repo, err := ggit.PlainInit(bootstrap, false)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if err := writeFile(filepath.Join(bootstrap, "README.md"), componentReadme(c)); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := writeFile(filepath.Join(bootstrap, ".gitignore"), componentIgnore(c.kind)); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := writeLefthookConfig(bootstrap, filepath.Join(root, c.path), root); err != nil {
		return plumbing.ZeroHash, err
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

func writeArtifacts(root string, cs []component) error {
	files := map[string]string{
		".gitignore":               "**/.DS_Store\n**/Thumbs.db\n**/desktop.ini\n\n.smt/\n",
		"README.md":                "# Platform workspace\n\nStart with [the documentation workspace](docs/README.md). Agents also read `AGENTS.md`.\n",
		".tool-versions":           toolVersions(cs),
		"AGENTS.md":                "# Project Agent Operating Agreement\n\nGo work uses `$godex:godex-go-backend`. Beads (`bd`) is the canonical task and issue state. The `work_manager` owns delivery decisions.\n\nWorkflow: `work_manager -> component worker -> tests -> manager review -> durable handoff/docs -> human E2E review -> release gate`.\n\nPrepared workspace commits must use `type(scope): [WORK-ID] summary`, with the bracketed Beads ID or assigned Jira alias immediately after the conventional prefix. The prepared workspace manifest is authoritative for repository ownership and allowed work-item references.\n",
		"agents/work_manager.toml": "name = \"work_manager\"\nmodel_reasoning_effort = \"high\"\n\n# Prepared workspace contract\ncommit_format = \"type(scope): [WORK-ID] summary\"\nmanifest_authority = \"prepared workspace\"\n",
		"agents/doc_writer.toml":   "name = \"doc_writer\"\nmodel_reasoning_effort = \"low\"\n",
		"prompts/build.md":         "# Build workflow\n\nUse `bd` for canonical task state.\n\nFor a prepared workspace, commits must use `type(scope): [WORK-ID] summary`. Use only the repository's assigned Beads IDs or Jira aliases from the prepared workspace manifest; the manifest is authoritative for ownership and integration work.\n",
		"docs/README.md":           "---\ntitle: Documentation Workspace\n---\n# Documentation Workspace\n\nUse [[00-project/Agentic Development Workflow]].\n",
		"docs/00-project/Agentic Development Workflow.md": "---\ntitle: Agentic Development Workflow\n---\n# Agentic Development Workflow\n\nBeads is canonical state.\n",
		"docs/Review Queue.base":                          "# View-only human-review evidence; Beads remains canonical state.\n",
	}
	for _, c := range cs {
		files["agents/"+c.id+"_worker.toml"] = "name = \"" + c.id + "_worker\"\nmodel_reasoning_effort = \"medium\"\n\ncommit_format = \"type(scope): [WORK-ID] summary\"\nprepared_workspace = \"Use assigned references from the prepared workspace manifest.\"\n"
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
func toolVersions(cs []component) string {
	v := []string{"task 3.52.0", "lefthook 2.1.10"}
	for _, c := range cs {
		switch c.id {
		case "api":
			v = append(v, "golang 1.26.5")
		case "web":
			v = append(v, "nodejs 24.18.0")
		case "mobile":
			v = append(v, "flutter 3.44.9")
		case "infra":
			v = append(v, "opentofu 1.12.3")
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
		return base + "\nbin/\ntmp/\n.env\n"
	case "database":
		return base + "\npostgres-data/\n.env\n"
	case "mobile":
		return base + "\n.dart_tool/\nbuild/\n.flutter-plugins\n.flutter-plugins-dependencies\n.packages\n"
	default:
		return base + "\n.terraform/\n.tofu/\n*.tfstate\n.env\n"
	}
}

func componentReadme(c component) string {
	if c.kind == "mobile" {
		return "# Flutter mobile application\n\nThis repository is a local SMT Flutter scaffold for Android and iOS.\n"
	}
	return "# " + c.title + "\n\nThis repository is a local SMT scaffold.\n"
}

// ValidateBlueprint accepts only the shape emitted by smt new. Remote URL
// changes are intentionally allowed after generation.
func ValidateBlueprint(cfg config.Config) error {
	if cfg.Version != 1 || cfg.Workspace.AIAssist != "codex" || cfg.Workflow == nil {
		return fmt.Errorf("apply requires an smt new blueprint")
	}
	wantTypes := []string{"feat", "fix", "refactor", "perf", "test", "docs", "build", "ci", "chore", "revert"}
	if strings.Join(cfg.Commit.Types, ",") != strings.Join(wantTypes, ",") {
		return fmt.Errorf("apply requires the default commit types")
	}
	if cfg.Providers.GitLab.APIBaseURL != "" || cfg.Providers.GitLab.EnterpriseBaseURL != "" || cfg.Providers.GitLab.EnterpriseUploadURL != "" || cfg.Providers.GitHub.APIBaseURL != "" || cfg.Providers.GitHub.EnterpriseBaseURL != "" || cfg.Providers.GitHub.EnterpriseUploadURL != "" || len(cfg.Contracts.Reference)+len(cfg.Contracts.Artifact)+len(cfg.Contracts.MigrationCoverage) != 0 {
		return fmt.Errorf("apply requires only supported blueprint fields")
	}
	if len(cfg.Repositories) < 2 || cfg.Repositories[0].ID != "repo" || cfg.Repositories[0].Path != "." || cfg.Repositories[0].Scope != "repo" || cfg.Repositories[0].Provider != "" || cfg.Repositories[0].Project != "" {
		return fmt.Errorf("apply requires the root blueprint repository")
	}
	if cfg.Repositories[0].HasChecks || len(cfg.Repositories[0].UnknownFields) != 0 {
		return fmt.Errorf("apply requires only supported blueprint fields")
	}
	expected := []component{{"web", "web-app", "web", "web", "nextjs", ""}, {"mobile", "mobile-app", "mobile", "mobile", "flutter", ""}, {"api", "apis", "api", "api", "go", ""}, {"database", "database", "database", "database", "postgresql", ""}, {"infra", "devops", "infra", "devops", "docker-opentofu", ""}}
	stacks := []string{cfg.Workspace.Stack.Web, cfg.Workspace.Stack.Mobile, cfg.Workspace.Stack.API, cfg.Workspace.Stack.Database, strings.Join(cfg.Workspace.Stack.DevOps, ",")}
	scopes := []string{"repo"}
	n := 1
	for i, e := range expected {
		if stacks[i] == "" {
			continue
		}
		if i == 1 && stacks[i] != "flutter" {
			return fmt.Errorf("apply requires the fixed mobile stack")
		}
		if e.id == "infra" && stacks[i] != "docker,opentofu" {
			return fmt.Errorf("apply requires the fixed devops stack")
		}
		if n >= len(cfg.Repositories) {
			return fmt.Errorf("apply repositories do not match selected stack")
		}
		r := cfg.Repositories[n]
		if r.ID != e.id || r.Path != e.path || r.Component != e.kind || r.Technology != e.tech || r.Scope != e.scope || r.Provider != "" || r.Project != "" || r.HasChecks || len(r.UnknownFields) != 0 {
			return fmt.Errorf("apply repositories do not match selected stack")
		}
		scopes = append(scopes, e.scope)
		n++
	}
	if n != len(cfg.Repositories) || strings.Join(scopes, ",") != strings.Join(cfg.Commit.Scopes, ",") {
		return fmt.Errorf("apply repositories do not match selected stack")
	}
	return nil
}
