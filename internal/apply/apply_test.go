package apply

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	ggit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/parmcoder/smt/internal/config"
)

func TestValidateBlueprintAcceptsExactNewConfiguration(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(`version: 1
workspace: {ai_assist: codex, stack: {web: nextjs}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBlueprint(*cfg); err != nil {
		t.Fatalf("ValidateBlueprint() error = %v", err)
	}
}

func TestValidateBlueprintAcceptsOnlyExactSelectedMobileMappingInOrder(t *testing.T) {
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBlueprint(*cfg); err != nil {
		t.Fatalf("ValidateBlueprint() error = %v", err)
	}
	for name, mutate := range map[string]func(*config.Config){
		"unsupported stack": func(cfg *config.Config) { cfg.Workspace.Stack.Mobile = "react-native" },
		"wrong id":          func(cfg *config.Config) { cfg.Repositories[2].ID = "app" },
		"wrong path":        func(cfg *config.Config) { cfg.Repositories[2].Path = "mobile" },
		"wrong component":   func(cfg *config.Config) { cfg.Repositories[2].Component = "web" },
		"wrong technology":  func(cfg *config.Config) { cfg.Repositories[2].Technology = "dart" },
		"wrong scope":       func(cfg *config.Config) { cfg.Repositories[2].Scope = "app" },
		"wrong order": func(cfg *config.Config) {
			cfg.Repositories[1], cfg.Repositories[2] = cfg.Repositories[2], cfg.Repositories[1]
		},
		"wrong commit order": func(cfg *config.Config) { cfg.Commit.Scopes = []string{"repo", "web", "api", "mobile"} },
	} {
		t.Run(name, func(t *testing.T) {
			copy := *cfg
			copy.Repositories = append([]config.Repository(nil), cfg.Repositories...)
			copy.Commit.Scopes = append([]string(nil), cfg.Commit.Scopes...)
			mutate(&copy)
			if err := ValidateBlueprint(copy); err == nil {
				t.Fatal("ValidateBlueprint() error=nil")
			}
		})
	}
}

func TestValidateBlueprintAcceptsSelectedMobileDatabaseWithoutDevOps(t *testing.T) {
	cfg, err := config.LoadBytes(mobileDatabaseBlueprintBytes(), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBlueprint(*cfg); err != nil {
		t.Fatalf("ValidateBlueprint() error = %v", err)
	}
}

func TestValidateBlueprintRejectsIncompleteDevOpsWithMobile(t *testing.T) {
	cfg, err := config.LoadBytes(mobileDevOpsBlueprintBytes(), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Workspace.Stack.DevOps = []string{"docker"}
	if err := ValidateBlueprint(*cfg); err == nil {
		t.Fatal("ValidateBlueprint() error=nil")
	}
}

func TestValidateBlueprintAcceptsFullSelectedMobileOrdering(t *testing.T) {
	cfg, err := config.LoadBytes(fullMobileBlueprintBytes(), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBlueprint(*cfg); err != nil {
		t.Fatalf("ValidateBlueprint() error = %v", err)
	}
	cfg.Repositories[3], cfg.Repositories[4] = cfg.Repositories[4], cfg.Repositories[3]
	if err := ValidateBlueprint(*cfg); err == nil {
		t.Fatal("ValidateBlueprint() error=nil for adjacent-order mismatch")
	}
}

func TestValidateBlueprintRejectsUnsupportedBlueprintExtensions(t *testing.T) {
	for name, replacement := range map[string]string{
		"provider":  "provider: github, project: example/web, ",
		"checks":    "checks: [], ",
		"generic":   "credential: secret, ",
		"contracts": "contracts: {artifact: [{id: artifact, repository: web, file: x, expected: y}]}",
	} {
		t.Run(name, func(t *testing.T) {
			raw := string(blueprintBytes())
			if name == "contracts" {
				raw += replacement + "\n"
			} else {
				raw = strings.Replace(raw, "id: web, ", "id: web, "+replacement, 1)
			}
			cfg, err := config.LoadBytes([]byte(raw), "/tmp/smt.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateBlueprint(*cfg); err == nil {
				t.Fatal("ValidateBlueprint() error=nil")
			}
		})
	}
}

func TestValidateBlueprintRejectsEveryProviderEndpointAndProfileForm(t *testing.T) {
	for _, endpoint := range []string{"gitlab.api_base_url", "gitlab.enterprise_base_url", "gitlab.enterprise_upload_url", "github.api_base_url", "github.enterprise_base_url", "github.enterprise_upload_url"} {
		t.Run(endpoint, func(t *testing.T) {
			parts := strings.Split(endpoint, ".")
			raw := string(blueprintBytes()) + "providers: {" + parts[0] + ": {" + parts[1] + ": https://example.invalid}}\n"
			cfg, err := config.LoadBytes([]byte(raw), "/tmp/smt.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateBlueprint(*cfg); err == nil {
				t.Fatal("ValidateBlueprint() error=nil")
			}
		})
	}
	for name, replacement := range map[string]string{"profile": "checks: {hook: []}, ", "legacy": "checks: [{kind: command, argv: [task]}], "} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(string(blueprintBytes()), "id: web, ", "id: web, "+replacement, 1)
			cfg, err := config.LoadBytes([]byte(raw), "/tmp/smt.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateBlueprint(*cfg); err == nil {
				t.Fatal("ValidateBlueprint() error=nil")
			}
		})
	}
}

func TestInitBeadsDoesNotExposeToolOutput(t *testing.T) {
	original := runBeads
	t.Cleanup(func() { runBeads = original })
	runBeads = func(context.Context, string, string, []string) ([]byte, error) {
		return []byte("SENTINEL_TOKEN_OUTPUT"), errors.New("exit status 1")
	}
	err := initBeadsWithPrefix(context.Background(), t.TempDir(), "workspace")
	if err == nil || strings.Contains(err.Error(), "SENTINEL_TOKEN_OUTPUT") {
		t.Fatalf("error=%v exposes tool output", err)
	}
}

func TestServiceBuildsCommittedSubmoduleTopology(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(blueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := Service{Config: *cfg, Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Beads: initializerFunc(func(_ context.Context, stage string) error {
		if err := os.MkdirAll(filepath.Join(stage, ".beads"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(stage, ".beads", "marker"), []byte("ok"), 0o600)
	})}
	if err := s.Apply(context.Background(), destination, blueprintBytes()); err != nil {
		t.Fatal(err)
	}
	repo, err := ggit.PlainOpen(destination)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(mustHead(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := tree.FindEntry("web-app")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Mode != filemode.Submodule {
		t.Fatalf("web-app mode=%s, want submodule", entry.Mode)
	}
	child, err := ggit.PlainOpen(filepath.Join(destination, "web-app"))
	if err != nil {
		t.Fatal(err)
	}
	head, err := child.Head()
	if err != nil {
		t.Fatal(err)
	}
	if entry.Hash != head.Hash() {
		t.Fatalf("gitlink=%s child=%s", entry.Hash, head.Hash())
	}
	remote, err := child.Remote(ggit.DefaultRemoteName)
	if err != nil || remote.Config().URLs[0] != filepath.Join(destination, ".smt", "bootstrap", "web") {
		t.Fatalf("published child origin=%v err=%v", remote, err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "smt.yaml"))
	if err != nil || string(got) != string(blueprintBytes()) {
		t.Fatalf("raw config mismatch: %q %v", got, err)
	}
	for _, path := range []string{".beads/marker", "docs/README.md", "AGENTS.md", "agents/work_manager.toml", "prompts/build.md", "README.md"} {
		if _, err := os.Stat(filepath.Join(destination, path)); err != nil {
			t.Fatalf("published %s: %v", path, err)
		}
	}
}

func TestServiceRegistersSubmodulesAsInitialized(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(blueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := Service{Config: *cfg, Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Beads: initializerFunc(func(context.Context, string) error { return nil })}
	if err := s.Apply(context.Background(), destination, blueprintBytes()); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("git", "-C", destination, "submodule", "status").CombinedOutput()
	if err != nil {
		t.Fatalf("git submodule status: %v: %s", err, output)
	}
	if strings.HasPrefix(string(output), "-") {
		t.Fatalf("submodule is uninitialized: %q", output)
	}
}

func TestServiceBuildsFlutterMobileSubmoduleAndArtifacts(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := Service{Config: *cfg, Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Beads: initializerFunc(func(context.Context, string) error { return nil })}
	if err := s.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
		t.Fatal(err)
	}
	repo, err := ggit.PlainOpen(destination)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := repo.CommitObject(mustHead(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	entry, err := tree.FindEntry("mobile-app")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Mode != filemode.Submodule {
		t.Fatalf("mobile-app mode=%s, want submodule", entry.Mode)
	}
	child, err := ggit.PlainOpen(filepath.Join(destination, "mobile-app"))
	if err != nil {
		t.Fatal(err)
	}
	head, err := child.Head()
	if err != nil {
		t.Fatal(err)
	}
	if entry.Hash != head.Hash() {
		t.Fatalf("gitlink=%s child=%s", entry.Hash, head.Hash())
	}
	remote, err := child.Remote(ggit.DefaultRemoteName)
	if err != nil || remote.Config().URLs[0] != filepath.Join(destination, ".smt", "bootstrap", "mobile") {
		t.Fatalf("published child origin=%v err=%v", remote, err)
	}
	readme, err := os.ReadFile(filepath.Join(destination, "mobile-app", "README.md"))
	if err != nil || !strings.Contains(string(readme), "Flutter") {
		t.Fatalf("mobile README=%q err=%v", readme, err)
	}
	ignore, err := os.ReadFile(filepath.Join(destination, "mobile-app", ".gitignore"))
	if err != nil || !strings.Contains(string(ignore), ".dart_tool/") || !strings.Contains(string(ignore), "build/") {
		t.Fatalf("mobile ignore=%q err=%v", ignore, err)
	}
	manifest, err := os.ReadFile(filepath.Join(destination, "agents", "mobile_worker.toml"))
	if err != nil || !strings.Contains(string(manifest), "mobile_worker") {
		t.Fatalf("mobile manifest=%q err=%v", manifest, err)
	}
	versions, err := os.ReadFile(filepath.Join(destination, ".tool-versions"))
	if err != nil || !strings.Contains(string(versions), "flutter 3.44.9\n") {
		t.Fatalf("tool versions=%q err=%v", versions, err)
	}
	raw, err := os.ReadFile(filepath.Join(destination, "smt.yaml"))
	if err != nil || string(raw) != string(mobileBlueprintBytes()) {
		t.Fatalf("raw config=%q err=%v", raw, err)
	}
}

func TestMobileAbsentLeavesExistingArtifactOutputUnchanged(t *testing.T) {
	cfg, err := config.LoadBytes(blueprintBytes(), "/tmp/smt.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := toolVersions(components(*cfg)), "task 3.52.0\nlefthook 2.1.10\nnodejs 24.18.0\n"; got != want {
		t.Fatalf("toolVersions()=%q, want %q", got, want)
	}
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	s := Service{Config: *cfg, Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Beads: initializerFunc(func(context.Context, string) error { return nil })}
	if err := s.Apply(context.Background(), destination, blueprintBytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "mobile-app")); !os.IsNotExist(err) {
		t.Fatalf("unexpected mobile app: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "agents", "mobile_worker.toml")); !os.IsNotExist(err) {
		t.Fatalf("unexpected mobile worker: %v", err)
	}
}

func blueprintBytes() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {web: nextjs}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}

func mobileBlueprintBytes() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {web: nextjs, mobile: flutter}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web, mobile]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}
  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}

func mobileDatabaseBlueprintBytes() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {web: nextjs, mobile: flutter, database: postgresql}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web, mobile, database]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}
  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, remote: {url: ""}}
  - {id: database, path: database, component: database, technology: postgresql, scope: database, remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}

func mobileDevOpsBlueprintBytes() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {web: nextjs, mobile: flutter, devops: [docker, opentofu]}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web, mobile, infra]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}
  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, remote: {url: ""}}
  - {id: infra, path: devops, component: devops, technology: docker-opentofu, scope: infra, remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}

func fullMobileBlueprintBytes() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {web: nextjs, mobile: flutter, api: go, database: postgresql, devops: [docker, opentofu]}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web, mobile, api, database, infra]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}
  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, remote: {url: ""}}
  - {id: api, path: apis, component: api, technology: go, scope: api, remote: {url: ""}}
  - {id: database, path: database, component: database, technology: postgresql, scope: database, remote: {url: ""}}
  - {id: infra, path: devops, component: devops, technology: docker-opentofu, scope: infra, remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}

func mustHead(t *testing.T, repository *ggit.Repository) plumbing.Hash {
	t.Helper()
	head, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	return head.Hash()
}

func TestServicePublishesRawConfigurationAfterPrerequisites(t *testing.T) {
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	raw := []byte("raw: preserved\n")
	s := Service{Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Initialize: initializerFunc(func(_ context.Context, stage string) error {
		return os.WriteFile(filepath.Join(stage, "marker"), []byte("ok"), 0o600)
	}), Beads: initializerFunc(func(context.Context, string) error { return nil })}
	if err := s.Apply(context.Background(), destination, raw); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "smt.yaml"))
	if err != nil || string(got) != string(raw) {
		t.Fatalf("raw=%q err=%v", got, err)
	}
}

func TestServiceFailuresLeaveNoDestination(t *testing.T) {
	for name, service := range map[string]Service{
		"prerequisites": {Prerequisites: prerequisiteFunc(func(context.Context) error { return errors.New("missing") })},
		"initializer":   {Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Initialize: initializerFunc(func(context.Context, string) error { return errors.New("git") })},
		"beads":         {Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Initialize: initializerFunc(func(context.Context, string) error { return nil }), Beads: initializerFunc(func(context.Context, string) error { return errors.New("beads") })},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			destination := filepath.Join(parent, "workspace")
			if err := service.Apply(context.Background(), destination, []byte("x")); err == nil {
				t.Fatal("Apply error=nil")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("destination=%v", err)
			}
			assertNoStage(t, parent)
		})
	}
}

func TestServiceStagedConfigurationWriteFailureCleansStaging(t *testing.T) {
	original := writeStagedConfig
	t.Cleanup(func() { writeStagedConfig = original })
	writeStagedConfig = func(string, []byte, os.FileMode) error { return errors.New("write") }
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	s := Service{Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Initialize: initializerFunc(func(context.Context, string) error { return nil }), Beads: initializerFunc(func(context.Context, string) error { return nil })}
	if err := s.Apply(context.Background(), destination, []byte("raw")); err == nil {
		t.Fatal("Apply error=nil")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination=%v", err)
	}
	assertNoStage(t, parent)
}

func TestServiceMobileBeadsAndPublishFailuresCleanStaging(t *testing.T) {
	for name, service := range map[string]Service{
		"beads": {
			Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
			Beads:         initializerFunc(func(context.Context, string) error { return errors.New("beads") }),
		},
		"publish": {
			Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
			Beads:         initializerFunc(func(context.Context, string) error { return nil }),
			Publish:       func(string, string) error { return errors.New("publish") },
		},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			destination := filepath.Join(parent, "workspace")
			cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			service.Config = *cfg
			if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err == nil {
				t.Fatal("Apply error=nil")
			}
			if _, err := os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatalf("destination=%v", err)
			}
			assertNoStage(t, parent)
		})
	}
}

func TestServiceRejectsExistingTargetsAndMissingParentWithoutStaging(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, target string){
		"file": func(t *testing.T, target string) {
			if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, target string) {
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, target string) {
			if err := os.Symlink("missing", target); err != nil {
				t.Fatal(err)
			}
		},
		"missing parent": func(t *testing.T, target string) {},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			target := filepath.Join(parent, "workspace")
			if name == "missing parent" {
				target = filepath.Join(parent, "missing", "workspace")
			} else {
				prepare(t, target)
			}
			s := Service{Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Initialize: initializerFunc(func(context.Context, string) error { return nil }), Beads: initializerFunc(func(context.Context, string) error { return nil })}
			if err := s.Apply(context.Background(), target, []byte("raw")); err == nil {
				t.Fatal("Apply error=nil")
			}
			if name != "missing parent" {
				if _, err := os.Lstat(target); err != nil {
					t.Fatalf("target changed: %v", err)
				}
			}
			assertNoStage(t, parent)
		})
	}
}

func TestServicePublishFailuresCleanStageAndPreserveTarget(t *testing.T) {
	for name, publish := range map[string]func(stage, target string) error{
		"injected failure": func(string, string) error { return errors.New("publish failed") },
		"target introduced": func(stage, target string) error {
			if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
				return err
			}
			return errors.New("target exists")
		},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			target := filepath.Join(parent, "workspace")
			s := Service{Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }), Initialize: initializerFunc(func(context.Context, string) error { return nil }), Beads: initializerFunc(func(context.Context, string) error { return nil }), Publish: publish}
			if err := s.Apply(context.Background(), target, []byte("raw")); err == nil {
				t.Fatal("Apply error=nil")
			}
			if name == "target introduced" {
				got, err := os.ReadFile(target)
				if err != nil || string(got) != "keep" {
					t.Fatalf("target=%q err=%v", got, err)
				}
			} else if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatalf("target=%v", err)
			}
			assertNoStage(t, parent)
		})
	}
}

func assertNoStage(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".smt-") {
			t.Fatalf("staging remnant %s", entry.Name())
		}
	}
}

type prerequisiteFunc func(context.Context) error

func (f prerequisiteFunc) Check(ctx context.Context) error { return f(ctx) }

type initializerFunc func(context.Context, string) error

func (f initializerFunc) Initialize(ctx context.Context, path string) error { return f(ctx, path) }
