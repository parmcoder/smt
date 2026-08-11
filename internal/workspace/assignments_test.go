package workspace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
)

func TestResolveAssignmentsGroupsReadyDirectChildrenInConfigurationOrder(t *testing.T) {
	cfg := assignmentConfig()
	feature := beads.Issue{ID: "smt-feature", Status: "open", Type: "feature"}
	children := []beads.Issue{
		{ID: "smt-b", Parent: feature.ID, Status: "open", Type: "task", Labels: []string{"repo:api"}, Title: "B", ExternalRef: "API-22"},
		{ID: "smt-a", Parent: feature.ID, Status: "open", Type: "task", Labels: []string{"repo:web"}, Title: "A", Description: "private description", Design: "private design", AcceptanceCriteria: "private acceptance"},
		{ID: "smt-c", Parent: "other", Status: "open", Type: "task", Labels: []string{"repo:api"}, Title: "not direct"},
	}

	got, err := ResolveAssignments(feature, cfg, children)
	if err != nil {
		t.Fatal(err)
	}
	if got.Feature.ID != feature.ID || len(got.Repositories) != 2 {
		t.Fatalf("assignments=%+v", got)
	}
	if ids := assignmentIDs(got.Repositories[0].Tasks); !reflect.DeepEqual(ids, []string{"smt-b"}) {
		t.Fatalf("api tasks=%v", ids)
	}
	if ids := assignmentIDs(got.Repositories[1].Tasks); !reflect.DeepEqual(ids, []string{"smt-a"}) {
		t.Fatalf("web tasks=%v", ids)
	}
	if got.Repositories[0].Tasks[0].AllowedReferences[1] != "API-22" {
		t.Fatalf("allowed refs=%v", got.Repositories[0].Tasks[0].AllowedReferences)
	}
}

func TestResolveAssignmentsPreservesCompleteTaskContext(t *testing.T) {
	feature := beads.Issue{ID: "feature", Status: "open", Type: "feature"}
	task := beads.Issue{
		ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:api"},
		Title: "Add API", Description: "description", Design: "design", AcceptanceCriteria: "criteria", ExternalRef: "PROJ-7",
	}
	got, err := ResolveAssignments(feature, assignmentConfig(), []beads.Issue{task})
	if err != nil {
		t.Fatal(err)
	}
	assignment := got.Repositories[0].Tasks[0]
	if assignment.Title != task.Title || assignment.Description != task.Description || assignment.Design != task.Design || assignment.AcceptanceCriteria != task.AcceptanceCriteria {
		t.Fatalf("task context=%+v", assignment)
	}
}

func TestResolveAssignmentsRejectsInvalidFeatureAndChildStates(t *testing.T) {
	cases := []struct {
		name     string
		feature  beads.Issue
		children []beads.Issue
		want     string
	}{
		{name: "closed feature", feature: beads.Issue{ID: "feature", Status: "closed", Type: "feature"}, want: "feature is not active"},
		{name: "wrong feature type", feature: beads.Issue{ID: "feature", Status: "open", Type: "task"}, want: "feature must be a feature"},
		{name: "missing repo label", feature: beads.Issue{ID: "feature", Status: "open", Type: "feature"}, children: []beads.Issue{{ID: "task", Parent: "feature", Status: "open", Type: "task"}}, want: "exactly one repository label"},
		{name: "unknown repo", feature: beads.Issue{ID: "feature", Status: "open", Type: "feature"}, children: []beads.Issue{{ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:unknown"}}}, want: "unknown repository"},
		{name: "multiple repo labels", feature: beads.Issue{ID: "feature", Status: "open", Type: "feature"}, children: []beads.Issue{{ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:api", "repo:web"}}}, want: "exactly one repository label"},
		{name: "blocked dependency", feature: beads.Issue{ID: "feature", Status: "open", Type: "feature"}, children: []beads.Issue{{ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:api"}, Dependencies: []beads.Issue{{ID: "blocker", Status: "open"}}}}, want: "dependency blocker is not closed"},
		{name: "invalid external ref", feature: beads.Issue{ID: "feature", Status: "open", Type: "feature"}, children: []beads.Issue{{ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:api"}, ExternalRef: "jira/7"}}, want: "invalid external reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveAssignments(tc.feature, assignmentConfig(), tc.children)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolveAssignmentsExcludesReviewAndDecisionRecords(t *testing.T) {
	feature := beads.Issue{ID: "feature", Status: "open", Type: "feature"}
	children := []beads.Issue{
		{ID: "review", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:api", "human-review", "e2e"}},
		{ID: "decision", Parent: "feature", Status: "open", Type: "decision", Labels: []string{"repo:api"}},
		{ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:api"}},
	}
	got, err := ResolveAssignments(feature, assignmentConfig(), children)
	if err != nil {
		t.Fatal(err)
	}
	if ids := assignmentIDs(got.Repositories[0].Tasks); !reflect.DeepEqual(ids, []string{"task"}) {
		t.Fatalf("tasks=%v", ids)
	}
}

func TestResolveAssignmentsErrorsAreSecretSafeAndJSONStable(t *testing.T) {
	secret := "token-and-private-description"
	feature := beads.Issue{ID: "feature", Status: "open", Type: "feature"}
	child := beads.Issue{ID: "task", Parent: "feature", Status: "open", Type: "task", Labels: []string{"repo:unknown"}, Description: secret}
	_, err := ResolveAssignments(feature, assignmentConfig(), []beads.Issue{child})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), "unknown repository") || !strings.Contains(err.Error(), "task") {
		t.Fatalf("error=%v", err)
	}
	first, firstErr := json.Marshal(assignmentConfig())
	second, secondErr := json.Marshal(assignmentConfig())
	if firstErr != nil || secondErr != nil || string(first) != string(second) {
		t.Fatalf("config JSON is not stable: %s / %s", first, second)
	}
}

func assignmentConfig() config.Config {
	return config.Config{Repositories: []config.Repository{
		{ID: "api", Path: "api", Scope: "api"},
		{ID: "web", Path: "web", Scope: "web"},
	}}
}

func assignmentIDs(tasks []TaskAssignment) []string {
	ids := make([]string, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	return ids
}
