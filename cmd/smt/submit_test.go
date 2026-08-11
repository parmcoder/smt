package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
	"github.com/sirupsen/logrus"
)

type submitCall struct {
	dir  string
	args []string
}

type submitRunner struct {
	counts map[string]int
	logs   map[string]string
	origin string
	calls  []submitCall
}

func (r *submitRunner) Run(_ context.Context, dir string, args ...string) (git.Result, error) {
	r.calls = append(r.calls, submitCall{dir: dir, args: append([]string(nil), args...)})
	if len(args) == 0 {
		return git.Result{}, errors.New("missing git command")
	}
	switch args[0] {
	case "rev-parse":
		return git.Result{Stdout: "true\n"}, nil
	case "status":
		return git.Result{}, nil
	case "symbolic-ref":
		return git.Result{Stdout: "feature/one\n"}, nil
	case "remote":
		if len(args) >= 3 && args[1] == "get-url" {
			return git.Result{Stdout: r.origin + "\n"}, nil
		}
	case "rev-list":
		return git.Result{Stdout: strconv.Itoa(r.counts[dir]) + "\n"}, nil
	case "ls-remote":
		return git.Result{Stdout: "base-sha\trefs/heads/main\n"}, nil
	case "log":
		return git.Result{Stdout: r.logs[dir]}, nil
	case "diff":
		if len(args) > 4 {
			return git.Result{Stdout: "api\n"}, nil
		}
		return git.Result{Stdout: "file.go\n"}, nil
	case "push":
		return git.Result{}, nil
	}
	return git.Result{}, errors.New("unexpected git command")
}

func TestWorkspaceSubmitCreatesChildThenRootReviews(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "api")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls"):
			_, _ = io.WriteString(w, "[]")
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			body, _ := io.ReadAll(r.Body)
			response := `{"number":7,"html_url":"https://reviews.example/7","title":"Review","body":"body","draft":false,"head":{"ref":"feature/one"},"base":{"ref":"main"}}`
			if strings.Contains(r.URL.Path, "/root/") && !strings.Contains(string(body), "https://reviews.example/7") {
				t.Fatalf("root body=%s", body)
			}
			_, _ = io.WriteString(w, response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("SMT_GITHUB_TOKEN", "submit-secret")
	cfg := submitConfig(server.URL)
	manifest := submitManifest(root)
	if _, err := workspacepkg.WriteRunManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	runner := submitRunner{
		counts: map[string]int{root: 1, child: 1},
		origin: "git@example:acme/root.git",
		logs: map[string]string{
			root:  "root-sha\x00feat(repo): [feature] integrate api\x00",
			child: "child-sha\x00feat(api): [smt-api-1] add endpoint\x00",
		},
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWorkspaceSubmit(context.Background(), cfg, root, "feature", &runner, true, false, true, out, errOut, logrus.New()); code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	var report submitOutput
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Pushed, []string{"api", "repo"}) {
		t.Fatalf("pushed=%v", report.Pushed)
	}
	if len(report.Reviews) != 2 || report.Reviews[0].Repository != "api" || report.Reviews[1].Repository != "repo" || report.Reviews[1].Status != "created" {
		t.Fatalf("reviews=%+v", report.Reviews)
	}
	if !reflect.DeepEqual(requests, []string{"GET /repos/acme/api/pulls", "POST /repos/acme/api/pulls", "GET /repos/acme/root/pulls", "POST /repos/acme/root/pulls"}) {
		t.Fatalf("provider requests=%v", requests)
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "push" {
			if call.dir != child && call.dir != root {
				t.Fatalf("unexpected push dir=%s", call.dir)
			}
		}
	}
}

func TestWorkspaceSubmitMissingTokenSucceedsWithHandoffAndDefersRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "api")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SMT_GITHUB_TOKEN", "")
	cfg := submitConfig("")
	manifest := submitManifest(root)
	if _, err := workspacepkg.WriteRunManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	runner := submitRunner{
		counts: map[string]int{root: 1, child: 1},
		origin: "git@example:acme/root.git",
		logs: map[string]string{
			root:  "root-sha\x00feat(repo): [feature] integrate api\x00",
			child: "child-sha\x00feat(api): [smt-api-1] add endpoint\x00",
		},
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWorkspaceSubmit(context.Background(), cfg, root, "feature", &runner, false, false, true, out, errOut, logrus.New()); code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	var report submitOutput
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Reviews) != 2 || report.Reviews[0].Status != "handoff" || report.Reviews[1].Status != "deferred" {
		t.Fatalf("reviews=%+v", report.Reviews)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Reviews[0].Body, "Closes `smt-api-1`") {
		t.Fatalf("handoff=%+v warnings=%v", report.Reviews[0], report.Warnings)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr=%s", errOut)
	}
}

func TestWorkspaceSubmitSkipsLocalOnlyReviewsAndStillCreatesRootReview(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "api")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls"):
			_, _ = io.WriteString(w, "[]")
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			_, _ = io.WriteString(w, `{"number":8,"html_url":"https://reviews.example/8","title":"Root review","body":"body","draft":true,"head":{"ref":"feature/one"},"base":{"ref":"main"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("SMT_GITHUB_TOKEN", "submit-secret")
	cfg := submitConfig(server.URL)
	cfg.Repositories[1].Provider = ""
	cfg.Repositories[1].Project = ""
	manifest := submitManifest(root)
	if _, err := workspacepkg.WriteRunManifest(root, manifest); err != nil {
		t.Fatal(err)
	}
	runner := submitRunner{
		counts: map[string]int{root: 1, child: 1},
		origin: "git@example:acme/root.git",
		logs: map[string]string{
			root:  "root-sha\x00feat(repo): [feature] integrate api\x00",
			child: "child-sha\x00feat(api): [smt-api-1] add endpoint\x00",
		},
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWorkspaceSubmit(context.Background(), cfg, root, "feature", &runner, false, false, true, out, errOut, logrus.New()); code != exitOK {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, out, errOut)
	}
	var report submitOutput
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Reviews) != 2 || report.Reviews[0].Repository != "api" || report.Reviews[0].Status != "local-only" || report.Reviews[1].Repository != "repo" || report.Reviews[1].Status != "created" {
		t.Fatalf("reviews=%+v", report.Reviews)
	}
	if !reflect.DeepEqual(requests, []string{"GET /repos/acme/root/pulls", "POST /repos/acme/root/pulls"}) {
		t.Fatalf("provider requests=%v", requests)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "no provider review configuration") {
		t.Fatalf("warnings=%v", report.Warnings)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr=%s", errOut)
	}
}

func submitConfig(apiBase string) config.Config {
	return config.Config{
		Version:   1,
		Providers: config.Providers{GitHub: config.ProviderConfig{APIBaseURL: apiBase}},
		Commit:    config.CommitConfig{Types: []string{"feat"}, Scopes: []string{"repo", "api"}},
		Repositories: []config.Repository{
			{ID: "repo", Path: ".", Scope: "repo", Provider: "github", Project: "acme/root", Remote: config.Remote{URL: "git@example:acme/root.git"}},
			{ID: "api", Path: "api", Scope: "api", Provider: "github", Project: "acme/api", Remote: config.Remote{URL: "git@example:acme/root.git"}},
		},
	}
}

func submitManifest(root string) workspacepkg.RunManifest {
	return workspacepkg.RunManifest{
		SchemaVersion: 1,
		Feature:       workspacepkg.FeatureContext{ID: "feature", Title: "Feature", Description: "feature summary", AcceptanceCriteria: "feature criteria"},
		WorkspacePath: root,
		Branch:        "feature/one",
		Repositories: []workspacepkg.ManifestRepository{
			{ID: "repo", Path: ".", BaseBranch: "main", BaseCommit: "repo-base", Ownership: "integration-worker", IntegrationGate: "root"},
			{ID: "api", Path: "api", BaseBranch: "main", BaseCommit: "api-base", Ownership: "repository-worker", IntegrationGate: "root-gitlink", Tasks: []workspacepkg.TaskAssignment{{ID: "smt-api-1", Title: "API task", Description: "API summary", AcceptanceCriteria: "API criteria", AllowedReferences: []string{"smt-api-1"}}}},
		},
	}
}
