package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/provider"
)

type provisionRunner struct {
	calls      []string
	origins    map[string]string
	addOrigins []string
	failAddDir string
}

func (r *provisionRunner) Run(_ context.Context, dir string, args ...string) (git.Result, error) {
	r.calls = append(r.calls, dir+" "+strings.Join(args, " "))
	if len(args) > 0 && args[0] == "rev-parse" {
		return git.Result{Stdout: "true\n"}, nil
	}
	if len(args) > 0 && args[0] == "symbolic-ref" {
		return git.Result{Stdout: "main\n"}, nil
	}
	if len(args) >= 3 && args[0] == "remote" && args[1] == "get-url" {
		if origin := r.origins[dir]; origin != "" {
			return git.Result{Stdout: origin + "\n"}, nil
		}
		return git.Result{ExitCode: 2}, errors.New("origin missing")
	}
	if len(args) >= 3 && args[0] == "remote" && args[1] == "add" {
		if dir == r.failAddDir {
			return git.Result{ExitCode: 1}, errors.New("origin add failed")
		}
		r.addOrigins = append(r.addOrigins, dir+"="+args[3])
	}
	return git.Result{}, nil
}

type provisionProvider struct {
	created []string
	specs   []provider.ProjectSpec
	inspect []string
}

func (p *provisionProvider) InspectProject(_ context.Context, project string) (provider.ProjectInfo, error) {
	p.inspect = append(p.inspect, project)
	return provider.ProjectInfo{Project: project}, nil
}

func (p *provisionProvider) CreateProject(_ context.Context, spec provider.ProjectSpec) (provider.ProjectInfo, error) {
	p.created = append(p.created, spec.Project)
	p.specs = append(p.specs, spec)
	return provider.ProjectInfo{Exists: true, Project: spec.Project, SSHURL: "git@example:" + spec.Project + ".git", WebURL: "https://example/" + spec.Project}, nil
}

func TestProvisionCreatesChildFirstAndWiresOnlyAfterAllProjectsExist(t *testing.T) {
	root := t.TempDir()
	childPath := filepath.Join(root, "api")
	if err := os.Mkdir(childPath, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := provisionConfig()
	runner := &provisionRunner{origins: map[string]string{}}
	projectProvider := &provisionProvider{}
	var factoryTokens []string
	report, err := Provision(context.Background(), cfg, root, runner, func(name string, settings config.ProviderConfig, token string) (provider.ProjectProvider, error) {
		factoryTokens = append(factoryTokens, name+"="+token)
		return projectProvider, nil
	}, func(name string) string { return name + "-token" }, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projectProvider.created, []string{"acme/api", "acme/root"}) {
		t.Fatalf("created=%v", projectProvider.created)
	}
	for _, spec := range projectProvider.specs {
		if spec.Visibility != "private" {
			t.Fatalf("visibility=%q for %s", spec.Visibility, spec.Project)
		}
	}
	if !reflect.DeepEqual(factoryTokens, []string{"github=github-token"}) {
		t.Fatalf("factory tokens=%v", factoryTokens)
	}
	if !reflect.DeepEqual(report.Configured, []string{"api", "repo"}) {
		t.Fatalf("configured=%v", report.Configured)
	}
	if report.Projects[0].Status != StatusCreated || report.Projects[1].Status != StatusCreated {
		t.Fatalf("project status=%+v", report.Projects)
	}
	if len(runner.addOrigins) != 2 || !strings.HasPrefix(runner.addOrigins[0], childPath+"=") {
		t.Fatalf("origin wiring=%v", runner.addOrigins)
	}
	loaded, err := config.Load(filepath.Join(root, "smt.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Repositories[0].Remote.URL != "git@example:acme/root.git" || loaded.Repositories[1].Remote.URL != "git@example:acme/api.git" {
		t.Fatalf("persisted remotes=%q %q", loaded.Repositories[0].Remote.URL, loaded.Repositories[1].Remote.URL)
	}
	gitmodules, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitmodules), "url = git@example:acme/api.git") {
		t.Fatalf("gitmodules=%s", gitmodules)
	}
}

func TestProvisionReportsCreatedAndPendingOnWiringFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &provisionRunner{origins: map[string]string{}, failAddDir: root}
	projectProvider := &provisionProvider{}
	report, err := Provision(context.Background(), provisionConfig(), root, runner, func(string, config.ProviderConfig, string) (provider.ProjectProvider, error) {
		return projectProvider, nil
	}, func(string) string { return "token" }, false)
	if err == nil || !strings.Contains(err.Error(), "configure repository origin") {
		t.Fatalf("error=%v", err)
	}
	if !reflect.DeepEqual(report.Configured, []string{"api"}) || !reflect.DeepEqual(report.Pending, []string{"repo"}) {
		t.Fatalf("progress=%+v", report)
	}
	if report.Projects[0].Status != StatusCreated || report.Projects[1].Status != StatusCreated {
		t.Fatalf("project status=%+v", report.Projects)
	}
}

func TestProvisionDryRunAndConflictAreMutationFree(t *testing.T) {
	root := t.TempDir()
	cfg := provisionConfig()
	runner := &provisionRunner{origins: map[string]string{}}
	projectProvider := &provisionProvider{}
	report, err := Provision(context.Background(), cfg, root, runner, func(string, config.ProviderConfig, string) (provider.ProjectProvider, error) {
		return projectProvider, nil
	}, func(string) string { return "token" }, true)
	if err != nil || !report.DryRun || len(projectProvider.created) != 0 {
		t.Fatalf("dry run report=%+v err=%v created=%v", report, err, projectProvider.created)
	}
	if _, err := os.Stat(filepath.Join(root, "smt.yaml")); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote config: %v", err)
	}

	runner.origins[filepath.Join(root, "api")] = "git@example:other/api.git"
	_, err = Provision(context.Background(), cfg, root, runner, func(string, config.ProviderConfig, string) (provider.ProjectProvider, error) {
		return projectProvider, nil
	}, func(string) string { return "token" }, false)
	if err == nil || !strings.Contains(err.Error(), "local remote conflict") {
		t.Fatalf("conflict error=%v", err)
	}
	if len(projectProvider.created) != 0 {
		t.Fatalf("conflict created projects=%v", projectProvider.created)
	}
}

func TestProvisionMissingTokenDoesNotContactProvider(t *testing.T) {
	root := t.TempDir()
	called := false
	_, err := Provision(context.Background(), provisionConfig(), root, &provisionRunner{origins: map[string]string{}}, func(string, config.ProviderConfig, string) (provider.ProjectProvider, error) {
		called = true
		return nil, nil
	}, func(string) string { return "" }, false)
	if err == nil || !strings.Contains(err.Error(), "token is not configured") || called {
		t.Fatalf("err=%v called=%t", err, called)
	}
}

func TestProvisionSupportsMixedProvidersInChildFirstOrder(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := provisionConfig()
	cfg.Repositories[0].Provider = "github"
	cfg.Repositories[1].Provider = "gitlab"
	cfg.Repositories[1].Project = "acme/group/api"
	runner := &provisionRunner{origins: map[string]string{}}
	providers := map[string]*provisionProvider{"github": {}, "gitlab": {}}
	var factoryOrder []string
	report, err := Provision(context.Background(), cfg, root, runner, func(name string, _ config.ProviderConfig, _ string) (provider.ProjectProvider, error) {
		factoryOrder = append(factoryOrder, name)
		return providers[name], nil
	}, func(name string) string { return name + "-token" }, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(factoryOrder, []string{"gitlab", "github"}) {
		t.Fatalf("factory order=%v", factoryOrder)
	}
	if !reflect.DeepEqual(providers["gitlab"].created, []string{"acme/group/api"}) || !reflect.DeepEqual(providers["github"].created, []string{"acme/root"}) {
		t.Fatalf("created gitlab=%v github=%v", providers["gitlab"].created, providers["github"].created)
	}
	if !reflect.DeepEqual(report.Configured, []string{"api", "repo"}) {
		t.Fatalf("configured=%v", report.Configured)
	}
}

func provisionConfig() config.Config {
	return config.Config{
		Version: 1,
		Commit:  config.CommitConfig{Types: []string{"feat"}, Scopes: []string{"repo", "api"}},
		Repositories: []config.Repository{
			{ID: "repo", Path: ".", Provider: "github", Project: "acme/root", Scope: "repo"},
			{ID: "api", Path: "api", Provider: "github", Project: "acme/api", Scope: "api"},
		},
	}
}
