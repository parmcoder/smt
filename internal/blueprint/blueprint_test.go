package blueprint

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestCreateDefaultBlueprintLoadsWithExpectedConfiguration(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	var out bytes.Buffer
	result, err := Create(strings.NewReader("\n\n\n\n\ny\n"), &out, destination)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Cancelled {
		t.Fatal("Create() cancelled, want published blueprint")
	}
	webPrompt := strings.Index(out.String(), "Include Web? [Y/n]")
	mobilePrompt := strings.Index(out.String(), "Include Flutter mobile application? [Y/n]")
	afterWeb := ""
	if webPrompt != -1 {
		afterWeb = out.String()[webPrompt+len("Include Web? [Y/n] "):]
	}
	if webPrompt == -1 || mobilePrompt == -1 || !strings.HasPrefix(afterWeb, "Include Flutter mobile application? [Y/n] ") {
		t.Fatalf("prompts = %q, want literal Mobile prompt immediately after Web", out.String())
	}
	cfg, err := config.Load(destination)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Workspace.AIAssist != "codex" || cfg.Workspace.Stack.Web != "nextjs" || cfg.Workspace.Stack.Mobile != "flutter" || cfg.Workspace.Stack.API != "go" || cfg.Workspace.Stack.Database != "postgresql" {
		t.Fatalf("workspace = %#v, want default selected fixed stack", cfg.Workspace)
	}
	if got, want := strings.Join(cfg.Workspace.Stack.DevOps, ","), "docker,opentofu"; got != want {
		t.Fatalf("devops = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.Commit.Scopes, ","), "repo,web,mobile,api,database,infra"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}
	if got, want := strings.Join(cfg.Commit.Types, ","), "feat,fix,refactor,perf,test,docs,build,ci,chore,revert"; got != want {
		t.Fatalf("types = %q, want %q", got, want)
	}
	wantRepositories := []struct {
		id, path, component, technology, scope string
	}{
		{"repo", ".", "", "", "repo"},
		{"web", "web-app", "web", "nextjs", "web"},
		{"mobile", "mobile-app", "mobile", "flutter", "mobile"},
		{"api", "apis", "api", "go", "api"},
		{"database", "database", "database", "postgresql", "database"},
		{"infra", "devops", "devops", "docker-opentofu", "infra"},
	}
	if len(cfg.Repositories) != len(wantRepositories) {
		t.Fatalf("repositories = %#v, want root and four selected components", cfg.Repositories)
	}
	for i, want := range wantRepositories {
		repository := cfg.Repositories[i]
		if repository.ID != want.id || repository.Path != want.path || repository.Component != want.component || repository.Technology != want.technology || repository.Scope != want.scope || repository.Remote.URL != "" {
			t.Fatalf("repository %d = %#v, want %#v with empty remote", i, repository, want)
		}
	}
	if cfg.Workflow.Policy.Manager != "work_manager" || cfg.Workflow.Policy.Implementation != "backend_worker" || cfg.Workflow.Policy.Documentation != "doc_writer" || !cfg.Workflow.Policy.ReviewRequired {
		t.Fatalf("workflow = %#v, want fixed workflow", cfg.Workflow)
	}
	wantPlugins := []config.WorkflowPlugin{{Source: "parmcoder/codex-obsidian", Selectors: []string{"codex-obsidian-writer", "codex-obsidian-markdown"}}, {Source: "parmcoder/godex", Selectors: []string{"godex-go-backend"}}}
	if len(cfg.Workflow.Plugins) != len(wantPlugins) {
		t.Fatalf("plugins = %#v, want %#v", cfg.Workflow.Plugins, wantPlugins)
	}
	for i, want := range wantPlugins {
		got := cfg.Workflow.Plugins[i]
		if got.Source != want.Source || strings.Join(got.Selectors, ",") != strings.Join(want.Selectors, ",") {
			t.Fatalf("plugin %d = %#v, want %#v", i, got, want)
		}
	}
}

func TestCreateRetriesAndUsesSelectedComponents(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "custom.yaml")
	var out bytes.Buffer
	_, err := Create(strings.NewReader("y\nperhaps\nn\nn\ny\nn\ny\n"), &out, destination)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	cfg, err := config.Load(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cfg.Commit.Scopes, ","), "repo,web,database"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "please answer yes or no") {
		t.Fatalf("output = %q, want retry hint", out.String())
	}
}

func TestCreateAllowsExplicitMobileOptOut(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	_, err := Create(strings.NewReader("y\nn\ny\nn\nn\ny\n"), &bytes.Buffer{}, destination)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	cfg, err := config.Load(destination)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.Stack.Mobile != "" {
		t.Fatalf("mobile stack = %q, want omitted", cfg.Workspace.Stack.Mobile)
	}
	if got, want := strings.Join(cfg.Commit.Scopes, ","), "repo,web,api"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}
}

func TestCreateAllNoAndInputEndDoNotWrite(t *testing.T) {
	for name, input := range map[string]string{"all no": "n\nn\nn\nn\nn\n", "component eof": "y\n"} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "smt.yaml")
			result, err := Create(strings.NewReader(input), &bytes.Buffer{}, destination)
			if err == nil || result.Cancelled {
				t.Fatalf("Create() result=%#v err=%v, want validation error", result, err)
			}
			if name == "all no" && !strings.Contains(err.Error(), "select at least one component") {
				t.Fatalf("error = %v, want component validation error", err)
			}
			if name == "component eof" && !strings.Contains(err.Error(), "input ended before confirmation") {
				t.Fatalf("error = %v, want stable EOF error", err)
			}
			if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("destination stat = %v, want no file", statErr)
			}
		})
	}
}

func TestCreateRejectsMissingParentAndPreservesExistingSymlink(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing", "smt.yaml")
	if _, err := Create(strings.NewReader(""), &bytes.Buffer{}, missing); err == nil || !strings.Contains(err.Error(), "parent") {
		t.Fatalf("Create() error = %v, want missing parent error", err)
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "smt.yaml")
	if err := os.Symlink(target, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(strings.NewReader(""), &bytes.Buffer{}, destination); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Create() error = %v, want symlink rejection", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "keep" {
		t.Fatalf("symlink target = %q, err=%v, want preserved", got, err)
	}
}

func TestCreateDeclineAndConfirmationEOFAreNoWriteCancellations(t *testing.T) {
	for name, input := range map[string]string{"decline": "y\ny\ny\ny\ny\nn\n", "confirmation eof": "y\ny\ny\ny\ny\n"} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "smt.yaml")
			result, err := Create(strings.NewReader(input), &bytes.Buffer{}, destination)
			if err != nil || !result.Cancelled {
				t.Fatalf("Create() result=%#v err=%v, want cancellation", result, err)
			}
			if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("destination stat = %v, want no file", statErr)
			}
		})
	}
}

func TestPublishRejectsExistingDestinationAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "smt.yaml")
	if err := os.WriteFile(destination, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publish(destination, []byte("replace"), nil); err == nil {
		t.Fatal("publish() error = nil, want existing destination rejection")
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != "keep" {
		t.Fatalf("destination = %q, err=%v, want preserved existing file", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %#v, err=%v, want only existing destination", entries, err)
	}
}

func TestPublishDoesNotOverwriteDestinationAppearingBeforePublish(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "smt.yaml")
	err := publish(destination, []byte("blueprint"), func() error {
		return os.WriteFile(destination, []byte("racer"), 0o600)
	})
	if err == nil {
		t.Fatal("publish() error = nil, want no-clobber failure")
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil || string(got) != "racer" {
		t.Fatalf("destination = %q, err=%v, want racing file preserved", got, readErr)
	}
	entries, readDirErr := os.ReadDir(dir)
	if readDirErr != nil || len(entries) != 1 {
		t.Fatalf("entries = %#v, err=%v, want only racing destination", entries, readDirErr)
	}
}
