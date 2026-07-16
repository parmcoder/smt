package contracts

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestValidateReportsAllContractKindsAndStableSeverityOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "reference.txt", "reference is current")
	writeFile(t, root, "source.sql", "source")
	writeFile(t, root, "migration.sql", "not ready")

	service := newService(t, root, config.Contracts{
		Reference: []config.Contract{
			{ID: "reference-pass", File: "reference.txt", Expected: "current", Replacement: "next", Severity: "warn"},
			{ID: "reference-fail", File: "reference.txt", Expected: "missing", Replacement: "next", Severity: "error"},
		},
		MigrationCoverage: []config.Contract{
			{ID: "migration-fail", File: "migration.sql", Source: "source.sql", Expected: "delivered", Severity: "error"},
			{ID: "source-fail", File: "missing-migration.sql", Source: "missing-source.sql", Expected: "delivered", Severity: "warn"},
		},
		Artifact: []config.Contract{
			{ID: "artifact-present", File: "reference.txt", Expected: "present", Severity: "warn"},
			{ID: "artifact-fail", File: "missing-artifact", Expected: "bundle", Severity: "error"},
		},
	})

	report, err := service.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got, want := report.Findings, []Finding{
		{ContractID: "artifact-fail", Type: "artifact", Severity: SeverityError, Message: "file does not exist", Path: "missing-artifact"},
		{ContractID: "migration-fail", Type: "migration-coverage", Severity: SeverityError, Message: "file does not contain expected migration marker", Path: "migration.sql"},
		{ContractID: "reference-fail", Type: "reference", Severity: SeverityError, Message: "file does not contain expected text", Path: "reference.txt"},
		{ContractID: "source-fail", Type: "migration-coverage", Severity: SeverityWarn, Message: "source file does not exist", Path: "missing-source.sql"},
		{ContractID: "source-fail", Type: "migration-coverage", Severity: SeverityWarn, Message: "file does not exist", Path: "missing-migration.sql"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Findings = %#v, want %#v", got, want)
	}
}

func TestValidatePassesAllContractKinds(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "reference.txt", "old new")
	writeFile(t, root, "source.sql", "source")
	writeFile(t, root, "migration.sql", "migration delivered")
	writeFile(t, root, "artifact.js", "bundle")

	service := newService(t, root, config.Contracts{
		Reference:         []config.Contract{{ID: "reference", File: "reference.txt", Expected: "old", Replacement: "new"}},
		MigrationCoverage: []config.Contract{{ID: "migration", File: "migration.sql", Source: "source.sql", Expected: "delivered"}},
		Artifact:          []config.Contract{{ID: "artifact", File: "artifact.js", Expected: "bundle"}},
	})
	report, err := service.Validate()
	if err != nil || len(report.Findings) != 0 {
		t.Fatalf("Validate() = (%#v, %v), want no findings", report.Findings, err)
	}
}

func TestAuditUsesValidationRulesWithoutMutation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "contract.txt", "before")
	service := newService(t, root, config.Contracts{
		Reference: []config.Contract{{ID: "reference", File: "contract.txt", Expected: "before", Replacement: "after"}},
	})

	before := readFile(t, root, "contract.txt")
	report, err := service.Audit()
	if err != nil || len(report.Findings) != 0 {
		t.Fatalf("Audit() = (%#v, %v), want no findings", report.Findings, err)
	}
	if got := readFile(t, root, "contract.txt"); got != before {
		t.Fatalf("Audit mutated file: got %q, want %q", got, before)
	}
}

func TestBumpPlanDoesNotWriteAndApplyRequiresExplicitApproval(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "contract.txt", "before")
	service := newService(t, root, config.Contracts{
		Reference: []config.Contract{{ID: "reference", File: "contract.txt", Expected: "before", Replacement: "after"}},
	})

	plan, err := service.PlanBump("reference")
	if err != nil {
		t.Fatalf("PlanBump() error = %v", err)
	}
	if plan.Before != "before" || plan.After != "after" || readFile(t, root, "contract.txt") != "before" {
		t.Fatalf("plan or file state = %#v, %q", plan, readFile(t, root, "contract.txt"))
	}
	if err := service.Apply(plan, false); err == nil {
		t.Fatal("Apply(false) error = nil, want explicit approval error")
	}
	if err := service.Apply(plan, true); err != nil {
		t.Fatalf("Apply(true) error = %v", err)
	}
	if got := readFile(t, root, "contract.txt"); got != "after" {
		t.Fatalf("applied file = %q, want after", got)
	}
}

func TestBumpGuardsUnknownTypeAmbiguityStalenessAndEscapes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "ambiguous.txt", "x x")
	writeFile(t, root, "stale.txt", "old")
	service := newService(t, root, config.Contracts{
		Reference: []config.Contract{
			{ID: "ambiguous", File: "ambiguous.txt", Expected: "x", Replacement: "y"},
			{ID: "stale", File: "stale.txt", Expected: "missing", Replacement: "new"},
		},
		Artifact: []config.Contract{{ID: "artifact", File: "stale.txt", Expected: "old"}},
	})

	for _, id := range []string{"unknown", "artifact", "ambiguous", "stale"} {
		if _, err := service.PlanBump(id); err == nil {
			t.Errorf("PlanBump(%q) error = nil", id)
		}
	}
	escape := newService(t, root, config.Contracts{Reference: []config.Contract{{ID: "escape", File: "../outside", Expected: "x", Replacement: "y"}}})
	if _, err := escape.PlanBump("escape"); err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("PlanBump(escape) error = %v, want workspace guard", err)
	}
}

func newService(t *testing.T, root string, contracts config.Contracts) *Service {
	t.Helper()
	service, err := New(root, contracts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
}

func readFile(t *testing.T, root, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", name, err)
	}
	return string(content)
}
