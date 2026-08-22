package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadAndValidate(t *testing.T) {
	valid := `version: 1
commit:
  types: [feat, fix]
  scopes: [api, web]
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: api
    checks:
      - kind: command
        argv: [task, verify]
  - id: web
    path: web
    provider: github
    project: sanovy/web
    scope: web
    checks:
      - kind: sql-format
        argv: [pg_format]
        include: ["**/*.sql"]
`

	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "valid", yaml: valid},
		{name: "wrong version", yaml: strings.Replace(valid, "version: 1", "version: 2", 1), want: "version must be 1"},
		{name: "duplicate id", yaml: strings.Replace(valid, "id: web", "id: root", 1), want: "duplicate repository id"},
		{name: "duplicate path", yaml: strings.Replace(valid, "path: web", "path: .", 1), want: "duplicate repository path"},
		{name: "duplicate cleaned path", yaml: strings.Replace(valid, "path: web", "path: ./", 1), want: "duplicate repository path"},
		{name: "duplicate project", yaml: strings.Replace(valid, "project: sanovy/web", "project: sanovy/root", 1), want: "duplicate repository project"},
		{name: "duplicate scope", yaml: strings.Replace(valid, "scope: web", "scope: api", 1), want: "duplicate repository scope"},
		{name: "missing root", yaml: strings.Replace(valid, "    path: .\n", "    path: root\n", 1), want: "exactly one root repository"},
		{name: "path escape", yaml: strings.Replace(valid, "path: web", "path: ../web", 1), want: "must remain inside workspace"},
		{name: "unknown provider", yaml: strings.Replace(valid, "provider: github", "provider: bitbucket", 1), want: "provider must be gitlab or github"},
		{name: "scope not declared", yaml: strings.Replace(valid, "scope: web", "scope: other", 1), want: "scope \"other\" is not declared"},
		{name: "empty command", yaml: strings.Replace(valid, "argv: [task, verify]", "argv: []", 1), want: "command check argv must not be empty"},
		{name: "empty command argument", yaml: strings.Replace(valid, "argv: [task, verify]", "argv: [task, \"\"]", 1), want: "check argv contains an empty argument"},
		{name: "empty sql formatter", yaml: strings.Replace(valid, "argv: [pg_format]", "argv: []", 1), want: "sql-format check argv must not be empty"},
		{name: "sql include missing", yaml: strings.Replace(valid, "        include: [\"**/*.sql\"]\n", "", 1), want: "sql-format check include must not be empty"},
		{name: "empty sql include", yaml: strings.Replace(valid, "include: [\"**/*.sql\"]", "include: [\"\"]", 1), want: "sql-format check include contains an empty pattern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "smt.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Load() error = %v", err)
				}
				if got.Version != 1 {
					t.Fatalf("Version = %d, want 1", got.Version)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRepositoryEffectiveDefaultBranchUsesMainWhenMissingOrBlank(t *testing.T) {
	for _, branch := range []string{"", "   ", "main", "trunk"} {
		repository := Repository{Remote: Remote{DefaultBranch: branch}}
		want := branch
		if strings.TrimSpace(want) == "" {
			want = "main"
		}
		if got := repository.EffectiveDefaultBranch(); got != want {
			t.Fatalf("branch=%q effective=%q want %q", branch, got, want)
		}
	}
}

func TestRepositoryRemoteDefaultBranchRoundTrips(t *testing.T) {
	raw := `version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    scope: repo
    remote:
      default_branch: trunk
`
	cfg, err := LoadBytes([]byte(raw), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Repositories[0].Remote.DefaultBranch; got != "trunk" {
		t.Fatalf("default branch=%q", got)
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "default_branch: trunk") {
		t.Fatalf("encoded=%s", encoded)
	}
}

func TestLoadAllowsRepeatedProvider(t *testing.T) {
	yaml := `version: 1
commit:
  types: [feat]
  scopes: [api, web]
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: api
  - id: web
    path: web
    provider: gitlab
    project: sanovy/web
    scope: web
`
	dir := t.TempDir()
	path := filepath.Join(dir, "smt.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v, want repeated providers to be valid", err)
	}
}

func TestRepositoryVisibilityAndQualifiedProjects(t *testing.T) {
	valid := `version: 1
commit: {types: [feat], scopes: [repo, api, platform]}
repositories:
  - {id: repo, path: ., provider: github, project: acme/api, visibility: public, scope: repo}
  - {id: platform, path: platform, provider: gitlab, project: acme/group/api, visibility: private, scope: platform}
  - {id: api, path: api, scope: api}
`
	cfg, err := Load(writeConfig(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Repositories[0].Visibility; got != "public" {
		t.Fatalf("visibility=%q", got)
	}
	if got := cfg.Repositories[1].EffectiveVisibility(); got != "private" {
		t.Fatalf("effective visibility=%q", got)
	}
	for name, raw := range map[string]string{
		"github project needs owner":     strings.Replace(valid, "project: acme/api", "project: api", 1),
		"gitlab project needs namespace": strings.Replace(valid, "project: acme/group/api", "project: api", 1),
		"invalid visibility":             strings.Replace(valid, "visibility: public", "visibility: internal", 1),
		"visibility needs provider":      strings.Replace(valid, "provider: github, project: acme/api, visibility: public", "project: acme/api, visibility: public", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, raw)); err == nil {
				t.Fatal("invalid provider configuration accepted")
			}
		})
	}
}

func TestRepositoryMarshalPreservesCheckProfiles(t *testing.T) {
	raw := `version: 1
commit: {types: [feat], scopes: [repo, api]}
repositories:
  - id: repo
    path: .
    scope: repo
    checks:
      hook:
        - kind: command
          argv: [task, hook]
      submit:
        - kind: command
          argv: [task, verify]
  - id: api
    path: api
    scope: api
    checks:
      - kind: command
        argv: [task, api]
`
	path := writeConfig(t, raw)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadBytes(encoded, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repositories[0].Profiles["hook"]) != 1 || len(got.Repositories[0].Profiles["submit"]) != 1 {
		t.Fatalf("profiles were not preserved: %+v", got.Repositories[0].Profiles)
	}
	if len(got.Repositories[1].Checks) != 1 || got.Repositories[1].Checks[0].Argv[0] != "task" {
		t.Fatalf("legacy checks were not preserved: %+v", got.Repositories[1].Checks)
	}
}

func TestLoadWorkspaceScaffoldConfiguration(t *testing.T) {
	yaml := `version: 1
workspace:
  ai_assist: codex
  stack:
    web: nextjs
    api: go
    database: postgresql
commit:
  types: [chore]
  scopes: [repo, web]
repositories:
  - id: repo
    path: .
    scope: repo
    remote:
      url: ""
  - id: web
    path: web-app
    component: web
    technology: nextjs
    scope: web
    remote:
      url: git@github.com:example/web-app.git
`

	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Workspace.AIAssist; got != "codex" {
		t.Fatalf("AIAssist = %q, want codex", got)
	}
	if got := cfg.Workspace.Stack.Database; got != "postgresql" {
		t.Fatalf("database stack = %q, want postgresql", got)
	}
	if got := cfg.Repositories[1].Remote.URL; got != "git@github.com:example/web-app.git" {
		t.Fatalf("remote URL = %q", got)
	}
}

func TestLoadMobileWorkspaceScaffoldConfiguration(t *testing.T) {
	yaml := `version: 1
workspace:
  ai_assist: codex
  stack:
    mobile: flutter
commit:
  types: [chore]
  scopes: [repo, mobile]
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, remote: {url: ""}}
`
	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Workspace.Stack.Mobile; got != "flutter" {
		t.Fatalf("mobile stack = %q, want flutter", got)
	}
}

func TestLoadVersionOneWithoutMobileRemainsValid(t *testing.T) {
	yaml := `version: 1
workspace:
  stack:
    web: nextjs
commit:
  types: [chore]
  scopes: [repo, web]
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}
`
	if _, err := Load(writeConfig(t, yaml)); err != nil {
		t.Fatalf("Load() error = %v, want version-1 Mobile-absent configuration to remain valid", err)
	}
}

func TestLoadRejectsInvalidMobileWorkspaceConfiguration(t *testing.T) {
	base := `version: 1
workspace:
  stack:
    mobile: flutter
commit:
  types: [chore]
  scopes: [repo, mobile]
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, remote: {url: ""}}
`
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "unsupported stack", yaml: strings.Replace(base, "mobile: flutter", "mobile: react-native", 1), want: "workspace.stack.mobile"},
		{name: "wrong repository ID", yaml: strings.Replace(base, "id: mobile", "id: handheld", 1), want: "mobile repository"},
		{name: "wrong repository path", yaml: strings.Replace(base, "path: mobile-app", "path: apps/mobile", 1), want: "mobile repository"},
		{name: "wrong repository component", yaml: strings.Replace(base, "component: mobile", "component: web", 1), want: "mobile repository"},
		{name: "wrong repository technology", yaml: strings.Replace(base, "technology: flutter", "technology: kotlin", 1), want: "mobile repository"},
		{name: "wrong repository scope", yaml: strings.Replace(base, "scope: mobile", "scope: app", 1), want: "mobile repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadWorkflowConfiguration(t *testing.T) {
	yaml := `version: 1
workspace:
  ai_assist: codex
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
workflow:
  policy:
    manager: work_manager
    implementation: backend_worker
    documentation: doc_writer
    review_required: true
  plugins:
    - source: parmcoder/codex-obsidian
      selectors: [codex-obsidian-writer, codex-obsidian-markdown]
    - source: parmcoder/godex
      selectors: [godex-go-backend]
`

	cfg, err := Load(writeConfig(t, yaml))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Workflow.Policy.Manager; got != "work_manager" {
		t.Fatalf("workflow manager = %q, want work_manager", got)
	}
}

func TestLoadBytesPreservesValidationContract(t *testing.T) {
	raw := []byte("version: 1\ncommit: {types: [feat], scopes: [repo]}\nrepositories:\n  - {id: repo, path: ., scope: repo, remote: {url: \"\"}}\n")
	if _, err := LoadBytes(raw, "/tmp/smt.yaml"); err != nil {
		t.Fatalf("LoadBytes() error = %v", err)
	}
}

func TestLoadRejectsLegacyDevOpsConfigurationWithMigrationError(t *testing.T) {
	legacy := map[string]string{
		"deprecated stack key": `version: 1
workspace:
  stack:
    devops: [docker, opentofu]
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
`,
		"legacy infra repository": `version: 1
commit: {types: [feat], scopes: [repo, infra]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: infra, path: devops, component: devops, technology: docker-opentofu, scope: infra, remote: {url: ""}}
`,
	}
	for name, suffix := range legacy {
		t.Run(name, func(t *testing.T) {
			_, err := LoadBytes([]byte(suffix), "/tmp/smt.yaml")
			if err == nil || !strings.Contains(err.Error(), "DevOps") || !strings.Contains(err.Error(), "remove") || !strings.Contains(err.Error(), "regenerate") {
				t.Fatalf("LoadBytes() error = %v, want migration-oriented DevOps removal/regeneration guidance", err)
			}
		})
	}
}

func TestLoadStillRejectsUnrelatedUnknownWorkspaceStackFields(t *testing.T) {
	raw := `version: 1
workspace:
  stack:
    experimental: value
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
`
	if _, err := LoadBytes([]byte(raw), "/tmp/smt.yaml"); err == nil || !strings.Contains(err.Error(), "experimental") {
		t.Fatalf("LoadBytes() error = %v, want unrelated unknown field rejection", err)
	}
}

func TestLoadAcceptsExactProvenanceMapping(t *testing.T) {
	raw := `version: 1
provenance:
  tool: smt
  smt_version: v0.1.0
  template_set_version: v1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
`
	cfg, err := LoadBytes([]byte(raw), "/tmp/smt.yaml")
	if err != nil {
		t.Fatalf("LoadBytes() error = %v, want exact provenance to load", err)
	}
	if cfg.Provenance == nil || cfg.Provenance.Tool != ProvenanceTool || cfg.Provenance.SMTVersion != ProvenanceSMTVersion || cfg.Provenance.TemplateSetVersion != ProvenanceTemplateSetVersion {
		t.Fatalf("provenance = %#v, want exact current provenance", cfg.Provenance)
	}
}

func TestLoadRejectsUnsupportedProvenanceValues(t *testing.T) {
	base := `version: 1
provenance:
  tool: smt
  smt_version: v0.1.0
  template_set_version: v1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
`
	for name, replacement := range map[string][2]string{
		"tool":                 {"tool: smt", "tool: other"},
		"smt version":          {"smt_version: v0.1.0", "smt_version: v9.9.9"},
		"template set version": {"template_set_version: v1", "template_set_version: v2"},
	} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(base, replacement[0], replacement[1], 1)
			_, err := LoadBytes([]byte(raw), "/tmp/smt.yaml")
			if err == nil || !strings.Contains(err.Error(), "provenance") {
				t.Fatalf("LoadBytes() error = %v, want clear provenance validation error", err)
			}
		})
	}
}

func TestLoadRejectsUnknownProvenanceFields(t *testing.T) {
	raw := `version: 1
provenance:
  tool: smt
  smt_version: v0.1.0
  template_set_version: v1
  unknown: value
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
`
	if _, err := LoadBytes([]byte(raw), "/tmp/smt.yaml"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("LoadBytes() error = %v, want unknown provenance field rejection", err)
	}
}

func TestRepositoryModulesRoundTripAndNoModulesRemainValid(t *testing.T) {
	raw := `version: 1
commit: {types: [feat], scopes: [repo, web]}
repositories:
  - {id: repo, path: ., scope: repo}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, modules: [web]}
`
	cfg, err := LoadBytes([]byte(raw), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories[1].Modules) != 1 || cfg.Repositories[1].Modules[0] != "web" {
		t.Fatalf("modules = %#v, want [web]", cfg.Repositories[1].Modules)
	}
	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := LoadBytes(encoded, "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTrip.Repositories[1].Modules) != 1 || roundTrip.Repositories[1].Modules[0] != "web" {
		t.Fatalf("round-trip modules = %#v, want [web]", roundTrip.Repositories[1].Modules)
	}
	noModules := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo}
`
	if _, err := LoadBytes([]byte(noModules), "/tmp/smt.yaml"); err != nil {
		t.Fatalf("no-modules configuration error = %v", err)
	}
}

func TestRepositoryModulesRejectUnknownAndDuplicateIDs(t *testing.T) {
	base := `version: 1
commit: {types: [feat], scopes: [repo, web]}
repositories:
  - {id: repo, path: ., scope: repo}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, modules: [%s]}
`
	for name, modules := range map[string]string{
		"unknown":   "missing",
		"duplicate": "web, web",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadBytes([]byte(fmt.Sprintf(base, modules)), "/tmp/smt.yaml"); err == nil || !strings.Contains(err.Error(), "module") {
				t.Fatalf("LoadBytes() error = %v, want module validation error", err)
			}
		})
	}
}

func TestRepositoryModulesAcceptNonSelectablePlatformMetadata(t *testing.T) {
	raw := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, modules: [container]}
`
	if _, err := LoadBytes([]byte(raw), "/tmp/smt.yaml"); err != nil {
		t.Fatalf("LoadBytes() error = %v, want known non-selectable metadata accepted", err)
	}
}

func TestRepositoryModulesValidateNonSelectableDependencies(t *testing.T) {
	base := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, modules: [%s]}
`
	if _, err := LoadBytes([]byte(fmt.Sprintf(base, "argocd")), "/tmp/smt.yaml"); err == nil || !strings.Contains(err.Error(), `requires capability "k8s"`) {
		t.Fatalf("LoadBytes() error = %v, want argocd dependency validation", err)
	}
	if _, err := LoadBytes([]byte(fmt.Sprintf(base, "argocd, k8s")), "/tmp/smt.yaml"); err != nil {
		t.Fatalf("LoadBytes() error = %v, want known platform metadata with satisfied dependency", err)
	}
}

func TestBuiltInModuleCatalogIsExactAndDeclarative(t *testing.T) {
	catalog := BuiltInModuleCatalog()
	if err := catalog.Validate(); err != nil {
		t.Fatalf("built-in catalog validation error = %v", err)
	}
	if catalog.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", catalog.SchemaVersion)
	}
	wantIDs := []string{"web", "mobile", "api", "database", "e2e", "container", "cicd", "observability", "iac", "k8s", "argocd"}
	if len(catalog.Modules) != len(wantIDs) {
		t.Fatalf("catalog IDs = %#v, want exactly %#v", catalog.Modules, wantIDs)
	}
	for i, want := range wantIDs {
		if catalog.Modules[i].ID != want {
			t.Fatalf("catalog module %d = %#v, want ID %q", i, catalog.Modules[i], want)
		}
		if catalog.Modules[i].Selectable != (i < 5) {
			t.Fatalf("catalog module %q selectable = %t, want %t", want, catalog.Modules[i].Selectable, i < 5)
		}
		if i >= 5 && (catalog.Modules[i].Category != "platform" || catalog.Modules[i].Layer != "platform-delivery") {
			t.Fatalf("platform module %q category/layer = %q/%q, want platform/platform-delivery", want, catalog.Modules[i].Category, catalog.Modules[i].Layer)
		}
		if len(catalog.Modules[i].CompletionCriteria) == 0 {
			t.Fatalf("catalog module %q lacks completion criteria", want)
		}
		if i < 5 && (len(catalog.Modules[i].Verification) == 0 || len(catalog.Modules[i].ScaffoldAssets) == 0 || len(catalog.Modules[i].Agents) == 0 || len(catalog.Modules[i].Skills) == 0) {
			t.Fatalf("selectable catalog module %q lacks declarative verification/assets/agent/skill fields: %#v", want, catalog.Modules[i])
		}
		if i >= 5 && len(catalog.Modules[i].ScaffoldAssets) != 0 {
			t.Fatalf("platform module %q has scaffold assets: %#v", want, catalog.Modules[i].ScaffoldAssets)
		}
		for _, verification := range catalog.Modules[i].Verification {
			if verification.ID == "" || len(verification.Argv) == 0 || verification.MutatesWorktree {
				t.Fatalf("catalog module %q has unsafe verification declaration: %#v", want, verification)
			}
		}
	}
	wantPlacements := map[string]ModuleRepositoryPlacement{
		"web":           {Path: "web-app", Scope: "web", Mode: "independent", Targets: []string{"web"}},
		"mobile":        {Path: "mobile-app", Scope: "mobile", Mode: "independent", Targets: []string{"mobile"}},
		"api":           {Path: "apis", Scope: "api", Mode: "independent", Targets: []string{"api"}},
		"database":      {Path: "database", Scope: "database", Mode: "independent", Targets: []string{"database"}},
		"e2e":           {Path: ".", Scope: "repo", Mode: "attached", Targets: []string{"repo"}},
		"container":     {Path: ".", Scope: "repo", Mode: "attached", Targets: []string{"web", "api"}},
		"cicd":          {Path: ".", Scope: "repo", Mode: "attached", Targets: []string{"repo", "web", "mobile", "api", "database"}},
		"observability": {Path: ".", Scope: "repo", Mode: "attached", Targets: []string{"web", "api", "database"}},
		"iac":           {Path: "platform/iac", Scope: "iac", Mode: "independent", Targets: []string{"repo"}},
		"k8s":           {Path: "platform/k8s", Scope: "k8s", Mode: "independent", Targets: []string{"repo"}},
		"argocd":        {Path: "platform/argocd", Scope: "argocd", Mode: "independent", Targets: []string{"repo"}},
	}
	for _, module := range catalog.Modules {
		want, ok := wantPlacements[module.ID]
		if !ok || !reflect.DeepEqual(module.Repository, want) {
			t.Fatalf("module %q placement = %#v, want %#v", module.ID, module.Repository, want)
		}
	}
	for _, module := range catalog.Modules[5:] {
		if strings.Join(module.Provides, ",") != module.ID {
			t.Fatalf("platform module %q provides = %v, want its own capability", module.ID, module.Provides)
		}
	}
	argocd := catalog.Modules[len(catalog.Modules)-1]
	if strings.Join(argocd.Requires, ",") != "k8s" {
		t.Fatalf("argocd requires = %v, want k8s", argocd.Requires)
	}
	for _, module := range catalog.Modules {
		if module.ID != "e2e" {
			continue
		}
		if strings.Join(module.Agents, ",") != "e2e_worker" {
			t.Fatalf("e2e agents = %v, want [e2e_worker]", module.Agents)
		}
		wantSkills := []string{"build-web-apps:frontend-testing-debugging", "flutter-add-integration-test"}
		if !reflect.DeepEqual(module.Skills, wantSkills) {
			t.Fatalf("e2e skills = %v, want %v", module.Skills, wantSkills)
		}
	}
}

func TestModuleCatalogRejectsArgocdWithoutK8sProvider(t *testing.T) {
	catalog := BuiltInModuleCatalog()
	filtered := catalog.Modules[:0]
	for _, module := range catalog.Modules {
		if module.ID != "k8s" {
			filtered = append(filtered, module)
		}
	}
	catalog.Modules = filtered
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), `unknown capability "k8s"`) {
		t.Fatalf("catalog.Validate() error = %v, want missing k8s provider error", err)
	}
}

func TestModuleCatalogRejectsInvalidPlacementAndCompletionCriteria(t *testing.T) {
	tests := map[string]func(*ModuleCatalog){
		"unsupported mode": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "component", []string{"web"}, []string{"build web"})
		},
		"missing mode": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "", []string{"web"}, []string{"build web"})
		},
		"empty target": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "independent", []string{""}, []string{"build web"})
		},
		"unknown target": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "independent", []string{"missing"}, []string{"build web"})
		},
		"duplicate target": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "independent", []string{"web", "web"}, []string{"build web"})
		},
		"missing completion criteria": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "independent", []string{"web"}, nil)
		},
		"blank completion criteria": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "independent", []string{"web"}, []string{" "})
		},
		"duplicate completion criteria": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "independent", []string{"web"}, []string{"web.declaration", "web.declaration"})
		},
		"prose completion criteria": func(catalog *ModuleCatalog) {
			setModuleContractForTest(t, catalog, "web", "independent", []string{"web"}, []string{"build web"})
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			catalog := BuiltInModuleCatalog()
			setValidModuleContractsForTest(t, &catalog)
			mutate(&catalog)
			err := catalog.Validate()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "module") && !strings.Contains(strings.ToLower(err.Error()), "placement") && !strings.Contains(strings.ToLower(err.Error()), "completion") {
				t.Fatalf("catalog.Validate() error = %v, want placement/completion validation error", err)
			}
		})
	}
}

func setValidModuleContractsForTest(t *testing.T, catalog *ModuleCatalog) {
	t.Helper()
	for i := range catalog.Modules {
		mode, targets := "independent", []string{catalog.Modules[i].ID}
		if catalog.Modules[i].ID == "e2e" {
			mode, targets = "attached", []string{"repo"}
		}
		setModuleContractForTest(t, catalog, catalog.Modules[i].ID, mode, targets, []string{catalog.Modules[i].ID + ".declaration"})
	}
}

func setModuleContractForTest(t *testing.T, catalog *ModuleCatalog, id, mode string, targets, criteria []string) {
	t.Helper()
	for i := range catalog.Modules {
		if catalog.Modules[i].ID != id {
			continue
		}
		catalog.Modules[i].Repository.Mode = mode
		catalog.Modules[i].Repository.Targets = targets
		catalog.Modules[i].CompletionCriteria = criteria
		return
	}
	t.Fatalf("module %q not found", id)
}

func TestModuleCatalogAcceptsAllFiveLayerVocabularyPairs(t *testing.T) {
	catalog := BuiltInModuleCatalog()
	shared := moduleDefinitionForTest("shared-extra", "control-plane", "control-plane", "shared-extra")
	shared.Repository.Mode = "shared"
	shared.Repository.Targets = []string{"repo"}
	catalog.Modules = append(catalog.Modules,
		moduleDefinitionForTest("control-extra", "control-plane", "control-plane", "control-extra"),
		moduleDefinitionForTest("platform-extra", "platform", "platform-delivery", "platform-extra"),
		shared,
	)
	if err := catalog.Validate(); err != nil {
		t.Fatalf("catalog.Validate() error = %v, want all five taxonomy pairs accepted", err)
	}
}

func TestModuleCatalogRejectsMismatchedLayerPair(t *testing.T) {
	catalog := BuiltInModuleCatalog()
	catalog.Modules = append(catalog.Modules, moduleDefinitionForTest("platform-invalid", "platform", "control-plane", "platform-invalid"))
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "category/layer") {
		t.Fatalf("catalog.Validate() error = %v, want category/layer mismatch", err)
	}
}

func TestQualityRootModuleUsesCatalogRoleAndRejectsAmbiguity(t *testing.T) {
	catalog := BuiltInModuleCatalog()
	for i := range catalog.Modules {
		if catalog.Modules[i].Category == "quality" && catalog.Modules[i].Repository.Path == "." && catalog.Modules[i].Repository.Scope == "repo" {
			catalog.Modules[i].ID = "quality-check"
		}
	}
	definition, err := QualityRootModule(catalog)
	if err != nil || definition.ID != "quality-check" {
		t.Fatalf("QualityRootModule() = %#v, error=%v, want mutated role definition", definition, err)
	}
	duplicate := definition
	duplicate.ID = "quality-alt"
	duplicate.Provides = []string{"quality-alt"}
	catalog.Modules = append(catalog.Modules, duplicate)
	if _, err := QualityRootModule(catalog); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("QualityRootModule() error = %v, want ambiguity error", err)
	}
}

func TestModuleCatalogRejectsWindowsDrivePathOnUnix(t *testing.T) {
	catalog := BuiltInModuleCatalog()
	catalog.Modules[0].Repository.Path = `C:\unsafe`
	if err := catalog.Validate(); err == nil || !strings.Contains(err.Error(), "must be relative") {
		t.Fatalf("catalog.Validate() error = %v, want Windows drive path rejection", err)
	}
}

func moduleDefinitionForTest(id, category, layer, capability string) ModuleDefinition {
	return ModuleDefinition{
		ID: id, Selectable: true, Category: category, Layer: layer, Provides: []string{capability},
		Repository: ModuleRepositoryPlacement{Path: id, Scope: id, Mode: "independent", Targets: []string{id}}, CompletionCriteria: []string{id + ".declaration"}, Agents: []string{id + "_worker"}, Skills: []string{id + "_skill"},
		Verification:   []VerificationRequirement{{ID: id + "-verify", Argv: []string{"task", "verify"}}},
		ScaffoldAssets: []ScaffoldAsset{{ID: id + "-asset", Path: id, Revision: "v1"}},
	}
}

func TestModuleCatalogRejectsMalformedDefinitions(t *testing.T) {
	tests := map[string]func(*ModuleCatalog){
		"unsupported schema": func(c *ModuleCatalog) { c.SchemaVersion = 2 },
		"duplicate IDs": func(c *ModuleCatalog) {
			c.Modules = append(c.Modules, c.Modules[0])
		},
		"unknown capability reference": func(c *ModuleCatalog) {
			c.Modules[4].Optional = append(c.Modules[4].Optional, "missing")
		},
		"unsafe placement path":       func(c *ModuleCatalog) { c.Modules[0].Repository.Path = "../outside" },
		"missing required capability": func(c *ModuleCatalog) { c.Modules[0].Requires = []string{"missing"} },
		"dependency cycle": func(c *ModuleCatalog) {
			c.Modules[0].Provides = []string{"alpha"}
			c.Modules[0].Requires = []string{"beta"}
			c.Modules[1].Provides = []string{"beta"}
			c.Modules[1].Requires = []string{"alpha"}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			catalog := BuiltInModuleCatalog()
			mutate(&catalog)
			if err := catalog.Validate(); err == nil || !strings.Contains(strings.ToLower(err.Error()), "module") && !strings.Contains(strings.ToLower(err.Error()), "catalog") {
				t.Fatalf("catalog.Validate() error = %v, want safe catalog validation error", err)
			}
		})
	}
}

func TestLoadRejectsInvalidWorkflowConfiguration(t *testing.T) {
	base := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`
	for name, replacement := range map[string]string{
		"wrong policy":        "manager: another_manager",
		"mismatched selector": "selectors: [codex-obsidian-writer]",
		"wrong source":        "source: another/plugin",
		"reordered selectors": "selectors: [codex-obsidian-markdown, codex-obsidian-writer]",
	} {
		t.Run(name, func(t *testing.T) {
			yaml := base
			if name == "wrong policy" {
				yaml = strings.Replace(yaml, "manager: work_manager", replacement, 1)
			} else if name == "mismatched selector" || name == "reordered selectors" {
				yaml = strings.Replace(yaml, "selectors: [codex-obsidian-writer, codex-obsidian-markdown]", replacement, 1)
			} else {
				yaml = strings.Replace(yaml, "source: parmcoder/codex-obsidian", replacement, 1)
			}
			if _, err := Load(writeConfig(t, yaml)); err == nil || !strings.Contains(err.Error(), "workflow") {
				t.Fatalf("Load() error = %v, want workflow validation failure", err)
			}
		})
	}
}

func TestLoadRejectsInvalidWorkspaceScaffoldConfiguration(t *testing.T) {
	base := `version: 1
workspace:
  ai_assist: codex
  stack:
    web: nextjs
commit:
  types: [chore]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    scope: repo
`
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{name: "unknown assistant", yaml: strings.Replace(base, "ai_assist: codex", "ai_assist: cursor", 1), want: "workspace.ai_assist"},
		{name: "unknown web stack", yaml: strings.Replace(base, "web: nextjs", "web: react", 1), want: "workspace.stack.web"},
		{name: "remote credentials", yaml: base + "    remote: {url: https://user:token@example.com/repo.git}\n", want: "remote.url must not contain credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadNamedProfilesAndContracts(t *testing.T) {
	yaml := `version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: command
          argv: [go, test, ./...]
      submit:
        - kind: sql-format
          argv: [pg_format]
          include: ["migrations/**/*.sql"]
          mutates_worktree: true
contracts:
  reference:
    - id: api-contract
      repository: root
      file: openapi.yaml
      expected: committed
      replacement: generated
      severity: warn
  migration-coverage:
    - id: migration-contract
      repository: root
      file: migrations/001.sql
      expected: applied
      source: schema.sql
  artifact:
    - id: artifact-contract
      repository: root
      file: dist/app.js
      expected: present
`
	path := writeConfig(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	profiles := cfg.Repositories[0].Profiles
	if len(profiles["hook"]) != 1 || len(profiles["submit"]) != 1 {
		t.Fatalf("Profiles = %#v, want hook and submit profiles", profiles)
	}
	if !profiles["submit"][0].MutatesWorktree {
		t.Fatal("submit SQL check mutation declaration = false, want true")
	}
	if got := cfg.Contracts.Reference[0].Severity; got != "warn" {
		t.Fatalf("reference severity = %q, want warn", got)
	}
	if got := cfg.Contracts.Artifact[0].Severity; got != "error" {
		t.Fatalf("artifact severity = %q, want default error", got)
	}
}

func TestValidateRejectsInvalidContracts(t *testing.T) {
	base := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: root, path: ., provider: gitlab, project: sanovy/root, scope: repo}
contracts:
  artifact:
    - {id: artifact, repository: missing, file: ../dist/app.js, expected: present}
`
	for name, want := range map[string]string{
		"unknown repository": "contract repository",
		"escaping file":      "contract file",
	} {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, strings.Replace(base, "repository: missing", "repository: root", 1))
			if name == "unknown repository" {
				path = writeConfig(t, base)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Load() error = %v, want substring %q", err, want)
			}
		})
	}
}

func TestValidateRejectsMutationOnUnknownCheckKind(t *testing.T) {
	yaml := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - id: root
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - {kind: unknown, argv: [echo, ok], mutates_worktree: true}
`
	_, err := Load(writeConfig(t, yaml))
	if err == nil || !strings.Contains(err.Error(), "unknown check kind") {
		t.Fatalf("Load() error = %v, want unknown check kind", err)
	}
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "smt.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
