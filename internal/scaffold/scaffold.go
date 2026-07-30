// Package scaffold creates the local, Git-backed workspace produced by smt init.
package scaffold

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
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
	codex, err := askYesNo(reader, out, "Use Codex assistance? [Y/n] ", true)
	if err != nil {
		return Selection{}, err
	}
	return Selection{Web: web, API: api, Database: database, DevOps: devops, Codex: codex}, nil
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

// Service creates Git repositories using argument-array Git commands.
type Service struct {
	runner git.Runner
}

// New creates an initialization service using runner.
func New(runner git.Runner) *Service {
	return &Service{runner: runner}
}

// Init creates an empty root repository and selected local bootstrap submodules.
func (s *Service) Init(ctx context.Context, destination string, selection Selection) (Result, error) {
	if s == nil || s.runner == nil {
		return Result{}, fmt.Errorf("initialize workspace: git runner is required")
	}
	components := selectedComponents(selection)
	if len(components) == 0 {
		return Result{}, fmt.Errorf("initialize workspace: select at least one component")
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return Result{}, fmt.Errorf("initialize workspace: resolve destination: %w", err)
	}
	if err := ensureEmpty(destination); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return Result{}, fmt.Errorf("initialize workspace: create destination: %w", err)
	}
	if err := s.run(ctx, destination, "init"); err != nil {
		return Result{}, err
	}
	if err := writeRootFiles(destination, selection, components); err != nil {
		return Result{}, err
	}
	for _, component := range components {
		if err := s.addBootstrapSubmodule(ctx, destination, component); err != nil {
			return Result{}, err
		}
	}
	if err := s.run(ctx, destination, "add", "-A"); err != nil {
		return Result{}, err
	}
	if err := s.commit(ctx, destination, "chore(repo): initialize workspace"); err != nil {
		return Result{}, err
	}

	result := Result{Destination: destination, Repositories: make([]string, 0, len(components)+1)}
	result.Repositories = append(result.Repositories, "repo")
	for _, component := range components {
		result.Repositories = append(result.Repositories, component.ID)
	}
	return result, nil
}

func (s *Service) addBootstrapSubmodule(ctx context.Context, root string, component component) error {
	bootstrap := filepath.Join(root, ".smt", "bootstrap", component.ID)
	if err := os.MkdirAll(bootstrap, 0o755); err != nil {
		return fmt.Errorf("initialize %s bootstrap: %w", component.ID, err)
	}
	if err := s.run(ctx, bootstrap, "init"); err != nil {
		return err
	}
	if err := writeComponentFiles(bootstrap, component); err != nil {
		return err
	}
	if err := s.run(ctx, bootstrap, "add", "-A"); err != nil {
		return err
	}
	if err := s.commit(ctx, bootstrap, "chore("+component.Scope+"): initialize "+component.ID); err != nil {
		return err
	}
	localURL := "./" + filepath.ToSlash(filepath.Join(".smt", "bootstrap", component.ID))
	if err := s.run(ctx, root, "-c", "protocol.file.allow=always", "submodule", "add", localURL, component.Path); err != nil {
		return err
	}
	return nil
}

func (s *Service) commit(ctx context.Context, dir, message string) error {
	return s.run(ctx, dir,
		"-c", "user.name="+bootstrapName,
		"-c", "user.email="+bootstrapEmail,
		"commit", "-m", message,
	)
}

func (s *Service) run(ctx context.Context, dir string, args ...string) error {
	result, err := s.runner.Run(ctx, dir, args...)
	if err != nil || result.ExitCode != 0 {
		message := strings.TrimSpace(result.Stderr)
		if message == "" {
			message = "git command failed"
		}
		if err != nil {
			return fmt.Errorf("initialize workspace: %w: %s", err, message)
		}
		return fmt.Errorf("initialize workspace: %s", message)
	}
	return nil
}

func ensureEmpty(destination string) error {
	entries, err := os.ReadDir(destination)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("initialize workspace: inspect destination: %w", err)
	}
	for _, entry := range entries {
		if !harmlessMetadata(entry.Name()) {
			return fmt.Errorf("initialize workspace: destination %s must be empty", destination)
		}
	}
	return nil
}

func harmlessMetadata(name string) bool {
	return name == ".DS_Store" || name == "Thumbs.db" || name == "desktop.ini"
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

func writeRootFiles(root string, selection Selection, components []component) error {
	if err := writeFile(filepath.Join(root, ".gitignore"), rootIgnore); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "README.md"), "# Platform workspace\n\nScaffolded by SMT. Select a worker through `AGENTS.md` to build each component.\n"); err != nil {
		return err
	}
	if err := writeConfig(filepath.Join(root, "smt.yaml"), selection, components); err != nil {
		return err
	}
	if !selection.Codex {
		return nil
	}
	if err := writeCodexFiles(root, components); err != nil {
		return err
	}
	return nil
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
		Commit       config.CommitConfig `yaml:"commit"`
		Repositories []config.Repository `yaml:"repositories"`
	}{
		Version:      1,
		Workspace:    config.Workspace{Stack: stack},
		Commit:       config.CommitConfig{Types: []string{"feat", "fix", "refactor", "perf", "test", "docs", "build", "ci", "chore", "revert"}, Scopes: scopes},
		Repositories: repositories,
	}
	if selection.Codex {
		generated.Workspace.AIAssist = "codex"
	}
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
	agreement := "# Project Agent Operating Agreement\n\nThe `work_manager` owns delivery decisions and assigns one component worker at a time. Workers implement only their repository and focused tests. `doc_writer` updates documentation after accepted behavior.\n\nWorkflow: `work_manager -> component worker -> tests -> work_manager review -> doc_writer -> final verification`.\n\nActive workers: " + strings.Join(workers, ", ") + ".\n"
	if err := writeFile(filepath.Join(root, "AGENTS.md"), agreement); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "agents", "work_manager.toml"), workManagerManifest); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, "agents", "doc_writer.toml"), docWriterManifest); err != nil {
		return err
	}
	return writeFile(filepath.Join(root, "prompts", "build.md"), "# Build workflow\n\n1. Read `AGENTS.md` and `smt.yaml`.\n2. The work manager writes one decision-complete assignment.\n3. The selected worker implements and tests only its component.\n4. The manager reviews before assigning the next component.\n5. The documentation worker records accepted behavior.\n")
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
