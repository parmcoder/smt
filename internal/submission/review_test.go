package submission

import (
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
)

func TestReviewContentUsesTaskReferencesAndChildLinks(t *testing.T) {
	manifest := workspacepkg.RunManifest{
		Feature:      workspacepkg.FeatureContext{ID: "smt-feature", Title: "Feature title", Description: "feature summary", AcceptanceCriteria: "feature criteria"},
		Repositories: []workspacepkg.ManifestRepository{{ID: "api", Tasks: []workspacepkg.TaskAssignment{{ID: "smt-api", ExternalRef: "API-7", Title: "API task", Description: "task summary", AcceptanceCriteria: "task criteria"}, {ID: "smt-api-2", Title: "Second", Description: "second summary", AcceptanceCriteria: "second criteria"}}}},
	}
	title, body, err := ReviewContent(manifest, "api", map[string]string{"web": "https://example/web/1"})
	if err != nil || title != "Feature title — api" {
		t.Fatalf("title=%q err=%v", title, err)
	}
	for _, want := range []string{"task summary", "task criteria", "Closes `smt-api`", "Closes `API-7`", "Closes `smt-api-2`", "web: https://example/web/1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestReviewLinkUsesProviderDefaultsAndCustomBases(t *testing.T) {
	if got := ReviewLink("github", config.ProviderConfig{}, "acme/api", "feature/one", "main"); !strings.Contains(got, "github.com/acme/api/compare/main") {
		t.Fatalf("github link=%q", got)
	}
	if got := ReviewLink("gitlab", config.ProviderConfig{APIBaseURL: "https://gitlab.example/api/v4/"}, "group/api", "feature/one", "main"); !strings.Contains(got, "gitlab.example/group/api/-/merge_requests/new") || !strings.Contains(got, "source_branch%5D=feature%2Fone") {
		t.Fatalf("gitlab link=%q", got)
	}
}
