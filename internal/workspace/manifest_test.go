package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
)

func TestBuildRunManifestRecordsEveryRepositoryAndBaseStateInConfigOrder(t *testing.T) {
	cfg := assignmentConfig()
	cfg.Repositories[0].Path = "."
	cfg.Repositories[0].Profiles = config.CheckProfiles{"submit": {}, "hook": {}}
	assignments, err := ResolveAssignments(
		beads.Issue{ID: "feature", Title: "Feature", Status: "open", Type: "feature"},
		cfg,
		[]beads.Issue{{ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:web"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildRunManifest(assignments, cfg, "/tmp/prepared", "feature/one", map[string]BaseState{
		"api": {Branch: "main", Commit: "api-sha"},
		"web": {Branch: "main", Commit: "web-sha"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || manifest.Feature.ID != "feature" || manifest.WorkspacePath != "/tmp/prepared" || manifest.Branch != "feature/one" {
		t.Fatalf("manifest=%+v", manifest)
	}
	if got := manifest.Repositories[0].ID; got != "api" {
		t.Fatalf("first repository=%q, want api", got)
	}
	if got := manifest.Repositories[1].Tasks[0].ID; got != "task" {
		t.Fatalf("web tasks=%+v", manifest.Repositories[1].Tasks)
	}
	if got := manifest.Repositories[0].BaseCommit; got != "api-sha" {
		t.Fatalf("api base commit=%q", got)
	}
	if got, want := manifest.Repositories[0].Ownership, "integration-worker"; got != want {
		t.Fatalf("root ownership=%q, want %q", got, want)
	}
	if got, want := manifest.Repositories[0].IntegrationGate, "root"; got != want {
		t.Fatalf("root integration gate=%q, want %q", got, want)
	}
	if got, want := manifest.Repositories[1].Ownership, "repository-worker"; got != want {
		t.Fatalf("child ownership=%q, want %q", got, want)
	}
	if got, want := manifest.Repositories[1].IntegrationGate, "root-gitlink"; got != want {
		t.Fatalf("child integration gate=%q, want %q", got, want)
	}
	if got, want := manifest.Repositories[0].CheckProfiles, []string{"hook", "submit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("root check profiles=%v, want %v", got, want)
	}
}

func TestWriteRunManifestIsDeterministicAndCreatesIgnoredRunDirectory(t *testing.T) {
	root := t.TempDir()
	manifest := RunManifest{SchemaVersion: 1, Feature: FeatureContext{ID: "feature", Title: "Feature"}, WorkspacePath: root, Branch: "feature/one"}
	path, err := WriteRunManifest(root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".smt", "runs", "feature.json")
	if path != want {
		t.Fatalf("manifest path=%q, want %q", path, want)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), `"schema_version": 1`) {
		t.Fatalf("manifest=%s", first)
	}
	if _, err := os.Stat(filepath.Join(root, ".smt", "runs")); err != nil {
		t.Fatal(err)
	}
	second, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil || !reflect.DeepEqual(first, append(second, '\n')) {
		t.Fatalf("manifest is not canonical: %s / %s", first, second)
	}
}

func TestWriteRunManifestRejectsUnsafeFeatureIDs(t *testing.T) {
	_, err := WriteRunManifest(t.TempDir(), RunManifest{SchemaVersion: 1, Feature: FeatureContext{ID: "../secret"}})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error=%v", err)
	}
}

func TestFindRunManifestRequiresExactFeatureAndBranch(t *testing.T) {
	root := t.TempDir()
	manifest := RunManifest{SchemaVersion: 1, Feature: FeatureContext{ID: "feature", Title: "Feature"}, WorkspacePath: root, Branch: "feature/one"}
	if _, err := WriteRunManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	got, err := FindRunManifest(root, "feature", "feature/one")
	if err != nil || got.Feature.ID != "feature" {
		t.Fatalf("manifest=%+v err=%v", got, err)
	}
	if _, err := FindRunManifest(root, "feature", "other"); err == nil {
		t.Fatal("branch mismatch accepted")
	}
}

func TestFindPreparedRepositoryMatchesCurrentPathAndBranch(t *testing.T) {
	root := t.TempDir()
	manifest := RunManifest{
		SchemaVersion: 1,
		Feature:       FeatureContext{ID: "feature", Title: "Feature"},
		WorkspacePath: root,
		Branch:        "feature/one",
		Repositories: []ManifestRepository{
			{ID: "repo", Path: ".", BaseBranch: "main", BaseCommit: "root-sha", Ownership: "integration-worker", IntegrationGate: "root"},
			{ID: "api", Path: "api", BaseBranch: "main", BaseCommit: "api-sha", Ownership: "repository-worker", IntegrationGate: "root-gitlink", Tasks: []TaskAssignment{{ID: "task", AllowedReferences: []string{"task"}}}},
		},
	}
	if _, err := WriteRunManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	got, repository, err := FindPreparedRepository(root, filepath.Join(root, "api"), "feature/one")
	if err != nil || repository.ID != "api" || got.Feature.ID != "feature" {
		t.Fatalf("manifest=%+v repository=%+v err=%v", got, repository, err)
	}
	if _, _, err := FindPreparedRepository(root, filepath.Join(root, "missing"), "feature/one"); err == nil {
		t.Fatal("unknown repository path accepted")
	}
}

func TestFindPreparedRepositoryFailsClosedForAmbiguousOrCorruptRuns(t *testing.T) {
	root := t.TempDir()
	for _, feature := range []string{"feature-a", "feature-b"} {
		manifest := RunManifest{SchemaVersion: 1, Feature: FeatureContext{ID: feature}, WorkspacePath: root, Branch: "feature/one", Repositories: []ManifestRepository{{ID: "repo", Path: ".", BaseBranch: "main", BaseCommit: feature + "-sha", Ownership: "integration-worker", IntegrationGate: "root"}}}
		if _, err := WriteRunManifest(root, manifest); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := FindPreparedRepository(root, root, "feature/one"); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous error=%v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".smt", "runs", "corrupt.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := FindPreparedRepository(root, root, "feature/one"); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("corrupt error=%v", err)
	}
}

func TestFindPreparedRepositoryRejectsSemanticallyCorruptRuns(t *testing.T) {
	tests := []struct {
		name         string
		repositories []ManifestRepository
	}{
		{
			name: "duplicate repository IDs",
			repositories: []ManifestRepository{
				{ID: "repo", Path: ".", BaseBranch: "main", BaseCommit: "root-sha"},
				{ID: "repo", Path: "api", BaseBranch: "main", BaseCommit: "api-sha"},
			},
		},
		{
			name: "missing base state",
			repositories: []ManifestRepository{
				{ID: "repo", Path: ".", BaseBranch: "main"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			runs := filepath.Join(root, ".smt", "runs")
			if err := os.MkdirAll(runs, 0o755); err != nil {
				t.Fatal(err)
			}
			manifest := RunManifest{
				SchemaVersion: 1,
				Feature:       FeatureContext{ID: "feature", Title: "Feature"},
				WorkspacePath: root,
				Branch:        "feature/one",
				Repositories:  test.repositories,
			}
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(runs, "feature.json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := FindRunManifest(root, "feature", "feature/one"); err == nil || !strings.Contains(err.Error(), "corrupt") {
				t.Fatalf("semantic corruption via feature lookup error=%v", err)
			}
			if _, _, err := FindPreparedRepository(root, root, "feature/one"); err == nil || !strings.Contains(err.Error(), "corrupt") {
				t.Fatalf("semantic corruption error=%v", err)
			}
		})
	}
}
