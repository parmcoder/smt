package commit

import (
	"strings"
	"testing"
)

func TestValidateMessage(t *testing.T) {
	validTypes := []string{"feat", "fix", "refactor", "perf", "test", "docs", "build", "ci", "chore", "revert"}
	for _, typ := range validTypes {
		t.Run(typ, func(t *testing.T) {
			if err := ValidateMessage(typ+"(api): add a thing\n\nBody text.\n\nRefs: #1\n", Policy{Types: validTypes, Scopes: []string{"api", "web"}}); err != nil {
				t.Fatalf("ValidateMessage() error = %v", err)
			}
		})
	}

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "missing scope", message: "feat: add a thing", want: "scope is required"},
		{name: "empty scope", message: "feat(): add a thing", want: "scope is required"},
		{name: "unknown type", message: "style(api): add a thing", want: "type \"style\" is not allowed"},
		{name: "unknown scope", message: "feat(ops): add a thing", want: "scope \"ops\" is not allowed"},
		{name: "empty multi scope", message: "feat(api,): add a thing", want: "scope contains an empty value"},
		{name: "unknown multi scope", message: "feat(api,ops): add a thing", want: "scope \"ops\" is not allowed"},
		{name: "malformed body", message: "feat(api): add a thing\nBody without separator", want: "invalid conventional commit"},
		{name: "malformed header", message: "feat(api) add a thing", want: "invalid conventional commit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMessage(tt.message, Policy{Types: validTypes, Scopes: []string{"api", "web"}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateMessage() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateBranchMessageUsesDefaultOrExactBranchID(t *testing.T) {
	policy := Policy{Types: []string{"feat"}, Scopes: []string{"api"}}
	if err := ValidateBranchMessage("feat(api): add endpoint", policy, "main", "main"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBranchMessage("feat(api): [task-7] add endpoint", policy, "task-7", "main"); err != nil {
		t.Fatal(err)
	}
	for _, message := range []string{"feat(api): add endpoint", "feat(api): [other] add endpoint"} {
		if err := ValidateBranchMessage(message, policy, "task-7", "main"); err == nil {
			t.Fatalf("accepted %q", message)
		}
	}
}
