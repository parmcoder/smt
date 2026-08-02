package scaffold

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	ggit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/prereq"
)

func TestInitWritesDiscoverableOrdinaryGitlinksWithoutSystemGit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "platform")
	if _, err := newReadyService(t).Init(context.Background(), root, Selection{Web: true, API: true, Codex: true}); err != nil {
		t.Fatal(err)
	}
	repository, err := ggit.PlainOpen(root)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[submodule \"web\"]", "path = web-app", "url = ./.smt/bootstrap/web", "[submodule \"api\"]", "path = apis"} {
		if !strings.Contains(string(contents), want) {
			t.Fatalf(".gitmodules missing %q: %s", want, contents)
		}
	}
	commit, err := repository.CommitObject(mustHead(t, repository))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"web-app", "apis"} {
		entry, err := tree.FindEntry(path)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Mode != filemode.Submodule {
			t.Fatalf("%s mode = %s", path, entry.Mode)
		}
		child, err := ggit.PlainOpen(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		origin, err := child.Remote(ggit.DefaultRemoteName)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := origin.Config().URLs, []string{filepath.Join(root, ".smt", "bootstrap", map[string]string{"web-app": "web", "apis": "api"}[path])}; len(got) != 1 || got[0] != want[0] {
			t.Fatalf("%s origin=%v want=%v", path, got, want)
		}
		head, err := child.Head()
		if err != nil {
			t.Fatal(err)
		}
		if entry.Hash != head.Hash() {
			t.Fatalf("%s gitlink=%s head=%s", path, entry.Hash, head.Hash())
		}
		if _, err := ggit.PlainOpen(origin.Config().URLs[0]); err != nil {
			t.Fatalf("%s published origin no longer resolves: %v", path, err)
		}
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	subs, err := worktree.Submodules()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 {
		t.Fatalf("submodules=%d", len(subs))
	}
}

func TestPromptRejectsInvalidAndMissingAnswers(t *testing.T) {
	for name, input := range map[string]string{
		"invalid": "maybe\n",
		"missing": "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Prompt(strings.NewReader(input), &strings.Builder{})
			if err == nil || !strings.Contains(err.Error(), "read init prompt") {
				t.Fatalf("Prompt() error=%v", err)
			}
		})
	}
}

func TestPromptCollectsSelection(t *testing.T) {
	selection, err := Prompt(strings.NewReader("y\nn\ny\nn\n"), &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if want := (Selection{Web: true, Database: true, Codex: true}); selection != want {
		t.Fatalf("selection=%#v want=%#v", selection, want)
	}
}

func TestInitRejectsNonEmptyButAcceptsHarmlessMetadata(t *testing.T) {
	nonempty := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonempty, "existing.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newReadyService(t).Init(context.Background(), nonempty, Selection{Web: true}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Init(nonempty) error=%v", err)
	}

	harmless := t.TempDir()
	if err := os.WriteFile(filepath.Join(harmless, ".DS_Store"), []byte("metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newReadyService(t).Init(context.Background(), harmless, Selection{Web: true}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Init(harmless) error=%v", err)
	}
}

func TestInitRequiresAtLeastOneComponent(t *testing.T) {
	_, err := newReadyService(t).Init(context.Background(), filepath.Join(t.TempDir(), "platform"), Selection{Codex: true})
	if err == nil || !strings.Contains(err.Error(), "at least one component") {
		t.Fatalf("Init() error=%v", err)
	}
}

func TestInitWritesAtomicWorkflowDocsAndConditionalPins(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "platform")
	fake := &fakePrerequisites{result: readyPrereqResult()}
	service := New(fake)
	if _, err := service.Init(context.Background(), destination, Selection{Web: true, API: true, DevOps: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(destination, "smt.yaml"))
	if err != nil || cfg.Workflow == nil || len(cfg.Workflow.RequiredPlugins) != 2 || cfg.Workspace.AIAssist != "codex" {
		t.Fatalf("workflow config=%#v err=%v", cfg, err)
	}
	pins, err := os.ReadFile(filepath.Join(destination, ".tool-versions"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(pins), "task 3.52.0\nlefthook 2.1.10\ngolang 1.26.5\nnodejs 24.18.0\nopentofu 1.12.3\n"; got != want {
		t.Fatalf("pins=%q want=%q", got, want)
	}
	if want := []prereq.Runtime{{Plugin: "task", Version: "3.52.0"}, {Plugin: "lefthook", Version: "2.1.10"}, {Plugin: "golang", Version: "1.26.5"}, {Plugin: "nodejs", Version: "24.18.0"}, {Plugin: "opentofu", Version: "1.12.3"}}; !reflect.DeepEqual(fake.requirements.Runtimes, want) || len(fake.requirements.Plugins) != 2 {
		t.Fatalf("requirements=%#v", fake.requirements)
	}
	if fake.prefix != "platform" {
		t.Fatalf("Beads prefix=%q", fake.prefix)
	}
	metadata, err := os.ReadFile(filepath.Join(destination, ".beads", "metadata.json"))
	if err != nil || !strings.Contains(string(metadata), `"prefix":"platform"`) {
		t.Fatalf("Beads metadata=%q err=%v", metadata, err)
	}
	repository, err := ggit.PlainOpen(destination)
	if err != nil {
		t.Fatal(err)
	}
	status, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	state, err := status.Status()
	if err != nil || !state.IsClean() {
		t.Fatalf("final worktree must be clean: status=%#v err=%v", state, err)
	}
	commit, err := repository.CommitObject(mustHead(t, repository))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := commit.Tree()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".beads/.gitignore", ".beads/README.md", ".beads/config.yaml", ".beads/metadata.json", ".beads/interactions.jsonl"} {
		if _, err := tree.FindEntry(path); err != nil {
			t.Fatalf("committed Beads file %s: %v", path, err)
		}
	}
	for _, path := range []string{"AGENTS.md", "prompts/build.md", "docs/README.md", "docs/00-project/Agentic Development Workflow.md", "docs/10-decisions", "docs/20-features", "docs/30-reviews", "docs/templates/Feature Handoff.md", "docs/templates/Human E2E Review.md", "docs/templates/Bug Report.md", "docs/Review Queue.base"} {
		if strings.HasSuffix(path, "10-decisions") || strings.HasSuffix(path, "20-features") || strings.HasSuffix(path, "30-reviews") {
			if info, err := os.Stat(filepath.Join(destination, path)); err != nil || !info.IsDir() {
				t.Fatalf("directory %s err=%v", path, err)
			}
			continue
		}
		contents, err := os.ReadFile(filepath.Join(destination, path))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if strings.HasPrefix(path, "docs/") && strings.HasSuffix(path, ".md") && (!strings.Contains(string(contents), "---") || !strings.Contains(string(contents), "[[")) {
			t.Fatalf("%s lacks Obsidian frontmatter or wikilink: %q", path, contents)
		}
	}
	agents, err := os.ReadFile(filepath.Join(destination, "AGENTS.md"))
	if err != nil || !strings.Contains(string(agents), "$godex:godex-go-backend") || !strings.Contains(string(agents), "Beads (`bd`) is the canonical task") || !strings.Contains(string(agents), "manager review -> durable handoff/docs -> human E2E review") || !strings.Contains(string(agents), "Agents must never approve or close human reviews") {
		t.Fatalf("agents=%q err=%v", agents, err)
	}
	readme, err := os.ReadFile(filepath.Join(destination, "README.md"))
	if err != nil || !strings.Contains(string(readme), "docs/README.md") || !strings.Contains(string(readme), "Agentic%20Development%20Workflow.md") || !strings.Contains(string(readme), "Review%20Queue.base") {
		t.Fatalf("README=%q err=%v", readme, err)
	}
	for path, fragments := range map[string][]string{
		"prompts/build.md": {"Use `bd` for canonical task state", "Manager review precedes", "Pass closes the review then the feature", "fail creates a linked bug", "re-queues the same review", "block release", "Agents never close human reviews"},
		"docs/00-project/Agentic Development Workflow.md": {"Beads is the canonical issue state", "Pass closes the review and then the feature", "same review for retest", "block release"},
		"docs/templates/Feature Handoff.md":               {"feature_issue:", "review_issue:"},
		"docs/templates/Human E2E Review.md":              {"type: human-e2e-review", "status: queued", "feature_issue:", "review_issue:", "bug_issue:", "reviewer:", "evidence:", "updated:", "retest-queued"},
		"docs/templates/Bug Report.md":                    {"bug_issue:", "feature_issue:", "discovered_from_review_issue:", "## Title", "## Reproduction", "## Expected behavior", "## Actual behavior", "## Evidence"},
		"docs/Review Queue.base":                          {"30-reviews", "View-only", "Beads remains canonical state"},
	} {
		contents, err := os.ReadFile(filepath.Join(destination, path))
		if err != nil {
			t.Fatal(err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(contents), fragment) {
				t.Fatalf("%s missing %q", path, fragment)
			}
		}
	}
}

func TestToolVersionsConditionalMatrix(t *testing.T) {
	for name, test := range map[string]struct {
		selection Selection
		want      string
	}{
		"database only": {Selection{Database: true}, "task 3.52.0\nlefthook 2.1.10\n"},
		"api only":      {Selection{API: true}, "task 3.52.0\nlefthook 2.1.10\ngolang 1.26.5\n"},
		"web only":      {Selection{Web: true}, "task 3.52.0\nlefthook 2.1.10\nnodejs 24.18.0\n"},
		"devops only":   {Selection{DevOps: true}, "task 3.52.0\nlefthook 2.1.10\nopentofu 1.12.3\n"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := toolVersions(test.selection); got != test.want {
				t.Fatalf("toolVersions()=%q want=%q", got, test.want)
			}
		})
	}
}

func TestInitPrecheckFailureWritesNoDestinationAndDoesNotInitBeads(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "platform")
	missing := &fakePrerequisites{result: prereq.Result{Findings: []prereq.Finding{{ID: "codex", Status: prereq.StatusMissing, Message: "Codex is not available", Guidance: "install it yourself"}}}}
	_, err := New(missing).Init(context.Background(), destination, Selection{Web: true})
	if err == nil || !strings.Contains(err.Error(), "Codex is not available") {
		t.Fatalf("Init() error=%v", err)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) || missing.initCalls != 0 {
		t.Fatalf("destination stat=%v initCalls=%d", statErr, missing.initCalls)
	}
}

func TestInitBeadsFailureAndAgentModificationLeaveNoPublishedDestination(t *testing.T) {
	for name, fake := range map[string]*fakePrerequisites{
		"beads failure":   {result: readyPrereqResult(), initErr: errors.New("beads failed")},
		"agents modified": {result: readyPrereqResult(), mutateAgents: true},
		"agents removed":  {result: readyPrereqResult(), removeAgents: true},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			destination := filepath.Join(parent, "platform")
			_, err := New(fake).Init(context.Background(), destination, Selection{Web: true})
			if err == nil || fake.initCalls != 1 {
				t.Fatalf("Init() error=%v calls=%d", err, fake.initCalls)
			}
			if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("destination stat=%v", statErr)
			}
			entries, readErr := os.ReadDir(parent)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("staging entries=%#v err=%v", entries, readErr)
			}
		})
	}
}

func TestInitPostBeadsFailureCleansStaging(t *testing.T) {
	parent := t.TempDir()
	service := newReadyService(t)
	service.afterBeads = func(context.Context, string) error { return errors.New("post-Beads failure") }
	if _, err := service.Init(context.Background(), filepath.Join(parent, "platform"), Selection{Web: true}); err == nil {
		t.Fatal("Init() error=nil")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging entries=%#v err=%v", entries, err)
	}
}

func TestInitRootAndArtifactFailuresCleanStaging(t *testing.T) {
	for name, configure := range map[string]func(*Service){
		"root init": func(service *Service) {
			service.initRoot = func(string) (*ggit.Repository, error) { return nil, errors.New("root init failed") }
		},
		"artifact write": func(service *Service) {
			service.writeRoot = func(string, Selection, []component) error { return errors.New("artifact write failed") }
		},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			service := newReadyService(t)
			configure(service)
			_, err := service.Init(context.Background(), filepath.Join(parent, "platform"), Selection{Web: true})
			if err == nil {
				t.Fatal("Init() error=nil")
			}
			entries, readErr := os.ReadDir(parent)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("staging entries=%#v err=%v", entries, readErr)
			}
		})
	}
}

type fakePrerequisites struct {
	result       prereq.Result
	checkErr     error
	initErr      error
	initCalls    int
	initDir      string
	prefix       string
	mutateAgents bool
	removeAgents bool
	requirements prereq.Requirements
}

func (f *fakePrerequisites) Check(_ context.Context, requirements prereq.Requirements) (prereq.Result, error) {
	f.requirements = requirements
	return f.result, f.checkErr
}

func (f *fakePrerequisites) InitBeads(_ context.Context, dir, prefix string) error {
	f.initCalls++
	f.initDir = dir
	f.prefix = prefix
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		return err
	}
	for path, contents := range map[string]string{
		".beads/.gitignore":         "embeddeddolt/\n",
		".beads/README.md":          "# Beads\n",
		".beads/config.yaml":        "issue-prefix: " + prefix + "\n",
		".beads/metadata.json":      "{\"prefix\":\"" + prefix + "\"}\n",
		".beads/interactions.jsonl": "",
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(contents), 0o600); err != nil {
			return err
		}
	}
	if f.mutateAgents {
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("changed"), 0o600); err != nil {
			return err
		}
	}
	if f.removeAgents {
		if err := os.Remove(filepath.Join(dir, "AGENTS.md")); err != nil {
			return err
		}
	}
	return f.initErr
}

func newReadyService(t *testing.T) *Service {
	t.Helper()
	return New(&fakePrerequisites{result: readyPrereqResult()})
}

func readyPrereqResult() prereq.Result {
	return prereq.Result{Findings: []prereq.Finding{{ID: "codex", Status: prereq.StatusReady}, {ID: "asdf", Status: prereq.StatusReady}, {ID: "bd", Status: prereq.StatusReady}}}
}
func mustHead(t *testing.T, repository *ggit.Repository) plumbing.Hash {
	t.Helper()
	head, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	return head.Hash()
}
