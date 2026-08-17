package blueprint

import (
	"bytes"
	"fmt"
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
	if got, want := strings.Join(cfg.Commit.Scopes, ","), "repo,web,mobile,api,database"; got != want {
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
	}
	if len(cfg.Repositories) != len(wantRepositories) {
		t.Fatalf("repositories = %#v, want root and four selected components", cfg.Repositories)
	}
	for i, want := range wantRepositories {
		repository := cfg.Repositories[i]
		if repository.ID != want.id || repository.Path != want.path || repository.Component != want.component || repository.Technology != want.technology || repository.Scope != want.scope || repository.Remote.URL != "" || repository.Remote.DefaultBranch != "main" {
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

func TestCreateAllComponentsOmitsDevOpsPromptAndArtifacts(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	var out bytes.Buffer
	result, err := Create(strings.NewReader("\n\n\n\n\ny\n"), &out, destination)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Cancelled {
		t.Fatal("Create() cancelled, want published blueprint")
	}
	if strings.Contains(out.String(), "Include DevOps") || strings.Contains(out.String(), "components: Web, Flutter mobile application, API, Database, DevOps") {
		t.Fatalf("prompts = %q, want no DevOps prompt", out.String())
	}
	cfg, err := config.Load(destination)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if got, want := strings.Join(cfg.Commit.Scopes, ","), "repo,web,mobile,api,database"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}
	if len(cfg.Repositories) != 5 {
		t.Fatalf("repositories = %#v, want root plus four components", cfg.Repositories)
	}
	for _, repository := range cfg.Repositories {
		if repository.ID == "infra" || repository.Component == "devops" || repository.Technology == "docker-opentofu" || isPlatformModuleID(repository.ID) {
			t.Fatalf("unexpected DevOps repository = %#v", repository)
		}
	}
}

func isPlatformModuleID(id string) bool {
	switch id {
	case "container", "cicd", "observability", "iac", "k8s", "argocd":
		return true
	default:
		return false
	}
}

func TestCreateAllowsExplicitMobileOptOutWithDeterministicSelection(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	var out bytes.Buffer
	result, err := Create(strings.NewReader("y\nn\ny\nn\nn\ny\n"), &out, destination)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Cancelled {
		t.Fatal("Create() cancelled, want published blueprint")
	}
	cfg, err := config.Load(destination)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	if cfg.Workspace.Stack.Mobile != "" {
		t.Fatalf("mobile stack = %q, want omitted", cfg.Workspace.Stack.Mobile)
	}
	if got, want := strings.Join(cfg.Commit.Scopes, ","), "repo,web,api"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}
}

func TestCreateE2EQualityDeclarationIsOptionalAndRootOnly(t *testing.T) {
	for name, input := range map[string]string{
		"default opt-out": "\n\n\n\nn\ny\n",
		"selected":        "\n\n\n\ny\ny\n",
	} {
		t.Run(name, func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "smt.yaml")
			var out bytes.Buffer
			if _, err := Create(strings.NewReader(input), &out, destination); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if !strings.Contains(out.String(), "Include E2E quality declaration? [y/N]") {
				t.Fatalf("prompts = %q, want catalog-driven E2E prompt", out.String())
			}
			cfg, err := config.Load(destination)
			if err != nil {
				t.Fatal(err)
			}
			root := cfg.Repositories[0]
			if name == "selected" {
				if len(root.Modules) != 1 || root.Modules[0] != "e2e" {
					t.Fatalf("root modules = %#v, want [e2e]", root.Modules)
				}
			} else if len(root.Modules) != 0 {
				t.Fatalf("root modules = %#v, want omitted for default opt-out", root.Modules)
			}
			for _, repository := range cfg.Repositories[1:] {
				if repository.ID == "e2e" || strings.Contains(repository.Path, "e2e") {
					t.Fatalf("unexpected E2E repository = %#v", repository)
				}
			}
		})
	}
}

func TestE2EPromptAndRootModuleUseCatalogDefinition(t *testing.T) {
	definition, err := e2eModuleDefinition()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	var out bytes.Buffer
	if _, err := Create(strings.NewReader("\n\n\n\ny\ny\n"), &out, destination); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	wantPrompt := fmt.Sprintf("Include %s %s declaration? [y/N]", strings.ToUpper(definition.ID), definition.Category)
	if !strings.Contains(out.String(), wantPrompt) {
		t.Fatalf("prompts = %q, want catalog-derived prompt %q", out.String(), wantPrompt)
	}
	cfg, err := config.Load(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories[0].Modules) != 1 || cfg.Repositories[0].Modules[0] != definition.ID {
		t.Fatalf("root modules = %#v, want catalog module %q", cfg.Repositories[0].Modules, definition.ID)
	}
}

func TestQualityRootLookupFollowsMutatedCatalogDefinition(t *testing.T) {
	original := moduleCatalogSource
	t.Cleanup(func() { moduleCatalogSource = original })
	catalog := config.BuiltInModuleCatalog()
	for i := range catalog.Modules {
		if catalog.Modules[i].Category == "quality" && catalog.Modules[i].Repository.Path == "." && catalog.Modules[i].Repository.Scope == "repo" {
			catalog.Modules[i].ID = "quality-check"
		}
	}
	moduleCatalogSource = func() config.ModuleCatalog { return catalog }
	definition, err := e2eModuleDefinition()
	if err != nil {
		t.Fatal(err)
	}
	if definition.ID != "quality-check" {
		t.Fatalf("quality root definition ID = %q, want mutated catalog ID", definition.ID)
	}
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	var out bytes.Buffer
	if _, err := Create(strings.NewReader("\n\n\n\ny\ny\n"), &out, destination); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	wantPrompt := fmt.Sprintf("Include %s %s declaration? [y/N]", strings.ToUpper(definition.ID), definition.Category)
	if !strings.Contains(out.String(), wantPrompt) {
		t.Fatalf("prompts = %q, want mutated catalog prompt %q", out.String(), wantPrompt)
	}
	raw, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "quality-check") || strings.Contains(string(raw), "modules: [e2e]") {
		t.Fatalf("generated config = %q, want mutated quality module ID", raw)
	}
}

func TestQualityRootLookupRejectsMissingOrAmbiguousRole(t *testing.T) {
	tests := map[string]func(*config.ModuleCatalog){
		"missing": func(catalog *config.ModuleCatalog) {
			for i := range catalog.Modules {
				if catalog.Modules[i].Category == "quality" {
					catalog.Modules[i].Category = "application"
					catalog.Modules[i].Layer = "application-components"
				}
			}
		},
		"ambiguous": func(catalog *config.ModuleCatalog) {
			for _, module := range catalog.Modules {
				if module.Category == "quality" && module.Repository.Path == "." && module.Repository.Scope == "repo" {
					module.ID = "quality-alt"
					module.Provides = []string{"quality-alt"}
					catalog.Modules = append(catalog.Modules, module)
					return
				}
			}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			original := moduleCatalogSource
			t.Cleanup(func() { moduleCatalogSource = original })
			catalog := config.BuiltInModuleCatalog()
			mutate(&catalog)
			moduleCatalogSource = func() config.ModuleCatalog { return catalog }
			if _, err := e2eModuleDefinition(); err == nil || !strings.Contains(err.Error(), "quality root module") {
				t.Fatalf("e2eModuleDefinition() error = %v, want safe role lookup error", err)
			}
		})
	}
}

func TestCreateEmitsDeterministicProvenance(t *testing.T) {
	var outputs [2][]byte
	for i := range outputs {
		destination := filepath.Join(t.TempDir(), "smt.yaml")
		if _, err := Create(strings.NewReader("y\nn\ny\ny\nn\ny\n"), &bytes.Buffer{}, destination); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		var err error
		outputs[i], err = os.ReadFile(destination)
		if err != nil {
			t.Fatal(err)
		}
		want := "provenance:\n    tool: smt\n    smt_version: v0.1.0\n    template_set_version: v1\n"
		if !strings.Contains(string(outputs[i]), want) {
			t.Fatalf("generated config = %q, want exact provenance mapping", outputs[i])
		}
	}
	if string(outputs[0]) != string(outputs[1]) {
		t.Fatalf("identical selections produced different bytes:\n%s\n---\n%s", outputs[0], outputs[1])
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
	for name, input := range map[string]string{"decline": "y\ny\ny\ny\nn\nn\n", "confirmation eof": "y\ny\ny\ny\nn\n"} {
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
