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

func TestValidatePreparedMessageRequiresAssignedReferenceImmediatelyAfterPrefix(t *testing.T) {
	policy := Policy{Types: []string{"feat", "fix"}, Scopes: []string{"api"}}
	allowed := []string{"smt-123", "API-7"}
	for _, message := range []string{
		"feat(api): [smt-123] add endpoint",
		"fix(api): [API-7] handle empty response",
	} {
		if err := ValidatePreparedMessage(message, policy, allowed); err != nil {
			t.Fatalf("ValidatePreparedMessage(%q) error = %v", message, err)
		}
	}
	for _, tc := range []struct {
		name, message, want string
	}{
		{name: "missing", message: "feat(api): add endpoint", want: "requires an assigned work-item reference"},
		{name: "not immediate", message: "feat(api): add [smt-123] endpoint", want: "requires an assigned work-item reference"},
		{name: "wrong repository", message: "feat(api): [WEB-1] add endpoint", want: "not assigned to this repository"},
		{name: "malformed", message: "feat(api): [bad/id] add endpoint", want: "invalid work-item reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePreparedMessage(tc.message, policy, allowed); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
