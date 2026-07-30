package scaffold

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
)

func TestPromptCollectsFixedPlatformProfile(t *testing.T) {
	var output bytes.Buffer
	selection, err := Prompt(strings.NewReader("y\nn\ny\nn\ny\n"), &output)
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if selection != (Selection{Web: true, Database: true, Codex: true}) {
		t.Fatalf("Selection = %#v", selection)
	}
	for _, prompt := range []string{"Next.js", "Go API", "PostgreSQL", "Docker and OpenTofu", "Codex"} {
		if !strings.Contains(output.String(), prompt) {
			t.Fatalf("output = %q, want prompt %q", output.String(), prompt)
		}
	}
}

func TestInitCreatesSelectedSubmoduleWorkspace(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "platform")
	result, err := New(git.ExecRunner{}).Init(context.Background(), destination, Selection{
		Web:   true,
		API:   true,
		Codex: true,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if got, want := result.Destination, destination; got != want {
		t.Fatalf("Destination = %q, want %q", got, want)
	}

	cfg, err := config.Load(filepath.Join(destination, "smt.yaml"))
	if err != nil {
		t.Fatalf("load generated config: %v", err)
	}
	if got := cfg.Workspace.AIAssist; got != "codex" {
		t.Fatalf("AIAssist = %q, want codex", got)
	}
	if len(cfg.Repositories) != 3 {
		t.Fatalf("repositories = %#v, want root plus web and api", cfg.Repositories)
	}
	for _, path := range []string{
		".gitmodules",
		".gitignore",
		"AGENTS.md",
		"agents/work_manager.toml",
		"agents/web_worker.toml",
		"agents/api_worker.toml",
		"agents/doc_writer.toml",
		"prompts/build.md",
		"web-app/.git",
		"apis/.git",
	} {
		if _, err := os.Stat(filepath.Join(destination, path)); err != nil {
			t.Fatalf("generated path %s: %v", path, err)
		}
	}
	gitmodules, err := os.ReadFile(filepath.Join(destination, ".gitmodules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitmodules), "web-app") || !strings.Contains(string(gitmodules), "apis") {
		t.Fatalf(".gitmodules = %s, want selected component paths", gitmodules)
	}
	state, err := git.Inspect(context.Background(), git.ExecRunner{}, git.Repository{ID: "root", Dir: destination, IsRoot: true})
	if err != nil {
		t.Fatalf("inspect root: %v", err)
	}
	if !state.Initialized || state.Dirty {
		t.Fatalf("root state = %#v, want initialized clean repository", state)
	}
}

func TestInitRejectsNonEmptyDestination(t *testing.T) {
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(destination, "existing.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(git.ExecRunner{}).Init(context.Background(), destination, Selection{Web: true, Codex: true})
	if err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("Init() error = %v, want non-empty destination refusal", err)
	}
}

func TestInitRequiresAtLeastOneComponent(t *testing.T) {
	_, err := New(git.ExecRunner{}).Init(context.Background(), filepath.Join(t.TempDir(), "platform"), Selection{Codex: true})
	if err == nil || !strings.Contains(err.Error(), "at least one component") {
		t.Fatalf("Init() error = %v, want component selection refusal", err)
	}
}
