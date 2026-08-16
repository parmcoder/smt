// Package blueprint builds a portable SMT configuration without creating a workspace.
package blueprint

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/parmcoder/smt/internal/config"
	"gopkg.in/yaml.v3"
)

var moduleCatalogSource = config.BuiltInModuleCatalog

// Selection holds the fixed component choices for a new configuration.
type Selection struct {
	Web      bool
	Mobile   bool
	API      bool
	Database bool
	E2E      bool
}

// Result describes a generated blueprint or a deliberate cancellation.
type Result struct {
	Destination string
	Cancelled   bool
}

// Create prompts for component choices and writes a validated blueprint only
// after explicit confirmation.
func Create(in io.Reader, out io.Writer, destination string) (Result, error) {
	destination, err := preflight(destination)
	if err != nil {
		return Result{}, err
	}
	reader := bufio.NewReader(in)
	selection, err := promptSelection(reader, out)
	if err != nil {
		return Result{}, err
	}
	if !selection.Web && !selection.Mobile && !selection.API && !selection.Database {
		return Result{}, errors.New("select at least one component")
	}
	data, err := marshal(selection)
	if err != nil {
		return Result{}, err
	}
	fmt.Fprintf(out, "blueprint target: %s\ncomponents: %s\n", destination, strings.Join(selection.labels(), ", "))
	confirmed, err := askConfirmation(reader, out)
	if err != nil {
		return Result{}, err
	}
	if !confirmed {
		fmt.Fprintln(out, "no file was written")
		return Result{Destination: destination, Cancelled: true}, nil
	}
	if err := publish(destination, data, nil); err != nil {
		return Result{}, err
	}
	fmt.Fprintf(out, "wrote blueprint %s\n", destination)
	return Result{Destination: destination}, nil
}

func preflight(destination string) (string, error) {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return "", fmt.Errorf("resolve destination: %w", err)
	}
	parent := filepath.Dir(abs)
	info, err := os.Stat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("destination parent %s does not exist", parent)
		}
		return "", fmt.Errorf("inspect destination parent: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("destination parent %s is not a directory", parent)
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", fmt.Errorf("destination %s already exists", abs)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect destination: %w", err)
	}
	return abs, nil
}

func promptSelection(reader *bufio.Reader, out io.Writer) (Selection, error) {
	web, err := askComponent(reader, out, "Web")
	if err != nil {
		return Selection{}, err
	}
	mobile, err := askComponent(reader, out, "Flutter mobile application")
	if err != nil {
		return Selection{}, err
	}
	api, err := askComponent(reader, out, "API")
	if err != nil {
		return Selection{}, err
	}
	database, err := askComponent(reader, out, "Database")
	if err != nil {
		return Selection{}, err
	}
	e2eModule, err := e2eModuleDefinition()
	if err != nil {
		return Selection{}, err
	}
	e2e, err := askOptionalModule(reader, out, e2eModule)
	if err != nil {
		return Selection{}, err
	}
	return Selection{Web: web, Mobile: mobile, API: api, Database: database, E2E: e2e}, nil
}

func askComponent(reader *bufio.Reader, out io.Writer, label string) (bool, error) {
	for {
		fmt.Fprintf(out, "Include %s? [Y/n] ", label)
		answer, ended, err := readAnswer(reader)
		if err != nil {
			return false, fmt.Errorf("read %s answer: %w", label, err)
		}
		if ended {
			return false, fmt.Errorf("input ended before confirmation while answering %s", label)
		}
		switch answer {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "please answer yes or no")
		}
	}
}

func askOptionalModule(reader *bufio.Reader, out io.Writer, module config.ModuleDefinition) (bool, error) {
	label := fmt.Sprintf("%s %s declaration", strings.ToUpper(module.ID), module.Category)
	for {
		fmt.Fprintf(out, "Include %s? [y/N] ", label)
		answer, ended, err := readAnswer(reader)
		if err != nil {
			return false, fmt.Errorf("read %s answer: %w", label, err)
		}
		if ended {
			return false, fmt.Errorf("input ended before confirmation while answering %s", label)
		}
		switch answer {
		case "", "n", "no":
			return false, nil
		case "y", "yes":
			return true, nil
		default:
			fmt.Fprintln(out, "please answer yes or no")
		}
	}
}

func e2eModuleDefinition() (config.ModuleDefinition, error) {
	return config.QualityRootModule(moduleCatalogSource())
}

func askConfirmation(reader *bufio.Reader, out io.Writer) (bool, error) {
	for {
		fmt.Fprint(out, "Write this blueprint? [y/N] ")
		answer, ended, err := readAnswer(reader)
		if err != nil {
			return false, fmt.Errorf("read confirmation: %w", err)
		}
		if ended {
			fmt.Fprintln(out, "no file was written")
			return false, nil
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "please answer yes or no")
		}
	}
}

func readAnswer(reader *bufio.Reader) (answer string, ended bool, err error) {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", false, err
	}
	answer = strings.ToLower(strings.TrimSpace(line))
	return answer, err == io.EOF, nil
}

func (s Selection) labels() []string {
	labels := make([]string, 0, 4)
	if s.Web {
		labels = append(labels, "Web")
	}
	if s.Mobile {
		labels = append(labels, "Flutter mobile application")
	}
	if s.API {
		labels = append(labels, "API")
	}
	if s.Database {
		labels = append(labels, "Database")
	}
	return labels
}

func marshal(selection Selection) ([]byte, error) {
	e2eModule, err := e2eModuleDefinition()
	if err != nil {
		return nil, err
	}
	stack := config.WorkspaceStack{}
	root := config.Repository{ID: "repo", Path: ".", Scope: "repo", Remote: config.Remote{DefaultBranch: "main"}}
	if selection.E2E {
		root.Modules = []string{e2eModule.ID}
	}
	repos := []config.Repository{root}
	scopes := []string{"repo"}
	if selection.Web {
		stack.Web = "nextjs"
		repos = append(repos, config.Repository{ID: "web", Path: "web-app", Component: "web", Technology: "nextjs", Scope: "web", Modules: []string{"web"}, Remote: config.Remote{DefaultBranch: "main"}})
		scopes = append(scopes, "web")
	}
	if selection.Mobile {
		stack.Mobile = "flutter"
		repos = append(repos, config.Repository{ID: "mobile", Path: "mobile-app", Component: "mobile", Technology: "flutter", Scope: "mobile", Modules: []string{"mobile"}, Remote: config.Remote{DefaultBranch: "main"}})
		scopes = append(scopes, "mobile")
	}
	if selection.API {
		stack.API = "go"
		repos = append(repos, config.Repository{ID: "api", Path: "apis", Component: "api", Technology: "go", Scope: "api", Modules: []string{"api"}, Remote: config.Remote{DefaultBranch: "main"}})
		scopes = append(scopes, "api")
	}
	if selection.Database {
		stack.Database = "postgresql"
		repos = append(repos, config.Repository{ID: "database", Path: "database", Component: "database", Technology: "postgresql", Scope: "database", Modules: []string{"database"}, Remote: config.Remote{DefaultBranch: "main"}})
		scopes = append(scopes, "database")
	}
	cfg := config.Config{Version: 1, Provenance: &config.Provenance{Tool: config.ProvenanceTool, SMTVersion: config.ProvenanceSMTVersion, TemplateSetVersion: config.ProvenanceTemplateSetVersion}, Workspace: config.Workspace{AIAssist: "codex", Stack: stack}, Commit: config.CommitConfig{Types: []string{"feat", "fix", "refactor", "perf", "test", "docs", "build", "ci", "chore", "revert"}, Scopes: scopes}, Repositories: repos, Workflow: fixedWorkflow()}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode blueprint: %w", err)
	}
	return data, nil
}

func fixedWorkflow() *config.Workflow {
	return &config.Workflow{Policy: config.WorkflowPolicy{Manager: "work_manager", Implementation: "backend_worker", Documentation: "doc_writer", ReviewRequired: true}, Plugins: []config.WorkflowPlugin{{Source: "parmcoder/codex-obsidian", Selectors: []string{"codex-obsidian-writer", "codex-obsidian-markdown"}}, {Source: "parmcoder/godex", Selectors: []string{"godex-go-backend"}}}}
}

func publish(destination string, data []byte, beforePublish func() error) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination %s already exists", destination)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".smt-new-*")
	if err != nil {
		return fmt.Errorf("create temporary blueprint: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure temporary blueprint: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary blueprint: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary blueprint: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary blueprint: %w", err)
	}
	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return fmt.Errorf("prepare publish: %w", err)
		}
	}
	if err := os.Link(tempName, destination); err != nil {
		return fmt.Errorf("publish blueprint without overwrite: %w", err)
	}
	return nil
}
