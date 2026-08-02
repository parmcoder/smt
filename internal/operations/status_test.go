package operations

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	ggit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/hooks"
)

func TestStatusContinuesAfterRepositoryErrorAndReportsDirtyState(t *testing.T) {
	root := newStatusRepository(t)
	if err := os.WriteFile(filepath.Join(root, "changed.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithHookInspector(config.Config{Repositories: []config.Repository{
		{ID: "missing", Path: filepath.Join(t.TempDir(), "missing")},
		{ID: "dirty", Path: root},
	}}, func(path string) (hooks.HookStatus, error) {
		if filepath.Base(path) == "missing" {
			return hooks.HookAbsent, nil
		}
		return hooks.HookCurrent, nil
	})
	entries, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Initialized || !entries[1].Initialized || !entries[1].Dirty || entries[1].HeadSHA == "" || entries[1].HookStatus != hooks.HookCurrent {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestStatusEntriesMarshalAsJSON(t *testing.T) {
	entries := []Entry{{ID: "repo", Path: ".", Initialized: true, HeadSHA: "abc123", Error: "diagnostic"}}
	if _, err := json.Marshal(entries); err != nil {
		t.Fatal(err)
	}
}

func newStatusRepository(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repository")
	repository, err := ggit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("initial", &ggit.CommitOptions{Author: &object.Signature{Name: "SMT Test", Email: "smt@example.invalid"}}); err != nil {
		t.Fatal(err)
	}
	return dir
}
