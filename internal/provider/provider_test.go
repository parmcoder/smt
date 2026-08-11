package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubProjectAndReviewContract(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if got := r.Header.Get("Authorization"); got != "Bearer github-secret" {
			t.Fatalf("authorization=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/api":
			_, _ = io.WriteString(w, `{"full_name":"acme/api","ssh_url":"git@github.com:acme/api.git","html_url":"https://github.com/acme/api"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/acme/repos":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["name"] != "new-api" || body["private"] != true || body["auto_init"] != nil {
				t.Fatalf("create body=%v err=%v", body, err)
			}
			_, _ = io.WriteString(w, `{"full_name":"acme/new-api","ssh_url":"git@github.com:acme/new-api.git","html_url":"https://github.com/acme/new-api"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/api/pulls":
			if r.URL.Query().Get("state") != "open" || r.URL.Query().Get("base") != "main" {
				t.Fatalf("review query=%s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `[{"number":7,"html_url":"https://github.com/acme/api/pull/7","title":"Draft","body":"body","draft":true,"head":{"ref":"feature/one"},"base":{"ref":"main"}}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/api/pulls":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["draft"] != true || body["head"] != "feature/one" {
				t.Fatalf("review body=%v err=%v", body, err)
			}
			_, _ = io.WriteString(w, `{"number":8,"html_url":"https://github.com/acme/api/pull/8","title":"New","body":"body","draft":true,"head":{"ref":"feature/one"},"base":{"ref":"main"}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/api/pulls/7":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["draft"] != false {
				t.Fatalf("ready body=%v err=%v", body, err)
			}
			_, _ = io.WriteString(w, `{"number":7,"html_url":"https://github.com/acme/api/pull/7","draft":false,"head":{"ref":"feature/one"},"base":{"ref":"main"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewGitHub(server.URL, "github-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.InspectProject(context.Background(), "acme/api")
	if err != nil || !project.Exists || project.SSHURL == "" || project.WebURL == "" {
		t.Fatalf("project=%+v err=%v", project, err)
	}
	created, err := client.CreateProject(context.Background(), ProjectSpec{Project: "acme/new-api", Visibility: "private"})
	if err != nil || created.Project != "acme/new-api" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	spec := ReviewSpec{Project: "acme/api", SourceBranch: "feature/one", TargetBranch: "main", Title: "New", Description: "body", Draft: true}
	reviews, err := client.FindOpenReviews(context.Background(), spec)
	if err != nil || len(reviews) != 1 || reviews[0].ID != "7" {
		t.Fatalf("reviews=%+v err=%v", reviews, err)
	}
	createdReview, err := client.CreateReview(context.Background(), spec)
	if err != nil || createdReview.ID != "8" {
		t.Fatalf("created review=%+v err=%v", createdReview, err)
	}
	if _, err := client.SetReady(context.Background(), reviews[0]); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 5 {
		t.Fatalf("requests=%v", requests)
	}
}

func TestGitLabProjectAndReviewContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "gitlab-secret" {
			t.Fatalf("private token=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/acme/group/api":
			_, _ = io.WriteString(w, `{"path_with_namespace":"acme/group/api","ssh_url_to_repo":"git@gitlab.com:acme/group/api.git","web_url":"https://gitlab.com/acme/group/api"}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/groups/"):
			_, _ = io.WriteString(w, `{"id":42,"full_path":"acme/group"}`)
		case r.Method == http.MethodPost && r.URL.Path == "/projects":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["namespace_id"] != float64(42) || body["initialize_with_readme"] != false || body["visibility"] != "public" {
				t.Fatalf("project body=%v err=%v", body, err)
			}
			_, _ = io.WriteString(w, `{"path_with_namespace":"acme/group/new-api","ssh_url_to_repo":"git@gitlab.com:acme/group/new-api.git","web_url":"https://gitlab.com/acme/group/new-api"}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge_requests"):
			_, _ = io.WriteString(w, `[{"iid":3,"web_url":"https://gitlab.com/acme/group/api/-/merge_requests/3","title":"Draft","description":"body","draft":true,"source_branch":"feature/one","target_branch":"main"}]`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge_requests"):
			_, _ = io.WriteString(w, `{"iid":4,"web_url":"https://gitlab.com/acme/group/api/-/merge_requests/4","draft":true,"source_branch":"feature/one","target_branch":"main"}`)
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge_requests/3"):
			_, _ = io.WriteString(w, `{"iid":3,"web_url":"https://gitlab.com/acme/group/api/-/merge_requests/3","draft":false,"source_branch":"feature/one","target_branch":"main"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewGitLab(server.URL, "gitlab-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.CreateProject(context.Background(), ProjectSpec{Project: "acme/group/new-api", Visibility: "public"})
	if err != nil || project.Project != "acme/group/new-api" {
		t.Fatalf("project=%+v err=%v", project, err)
	}
	spec := ReviewSpec{Project: "acme/group/api", SourceBranch: "feature/one", TargetBranch: "main", Title: "New", Description: "body", Draft: true}
	reviews, err := client.FindOpenReviews(context.Background(), spec)
	if err != nil || len(reviews) != 1 || reviews[0].ID != "3" {
		t.Fatalf("reviews=%+v err=%v", reviews, err)
	}
	created, err := client.CreateReview(context.Background(), spec)
	if err != nil || created.ID != "4" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	if _, err := client.SetReady(context.Background(), reviews[0]); err != nil {
		t.Fatal(err)
	}
}

func TestProviderErrorsNeverExposeTokenOrResponseBody(t *testing.T) {
	secret := "provider-response-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, secret)
	}))
	defer server.Close()
	client, err := NewGitHub(server.URL, "token-secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.InspectProject(context.Background(), "acme/api")
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "token-secret") {
		t.Fatalf("error=%v", err)
	}
}
