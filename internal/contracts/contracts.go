// Package contracts validates configured repository contracts and plans guarded reference bumps.
package contracts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parmcoder/smt/internal/config"
)

// Severity identifies the effect of a contract finding.
type Severity string

const (
	// SeverityError identifies a finding that should block the caller.
	SeverityError Severity = "error"
	// SeverityWarn identifies a finding that should be reported without blocking.
	SeverityWarn Severity = "warn"
)

// Finding describes one failed contract check.
type Finding struct {
	ContractID string
	Type       string
	Severity   Severity
	Message    string
	Path       string
}

// Report contains all findings from one contract inspection.
type Report struct {
	Findings []Finding
}

// HasErrors reports whether the report contains an error-severity finding.
func (r Report) HasErrors() bool {
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Service evaluates contracts relative to one explicit workspace root.
type Service struct {
	root      string
	contracts config.Contracts
}

// New creates a contract service rooted at workspaceRoot.
func New(workspaceRoot string, contracts config.Contracts) (*Service, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root is not a directory")
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return &Service{root: filepath.Clean(root), contracts: contracts}, nil
}

// Validate checks every configured contract and returns findings in stable order.
func (s *Service) Validate() (Report, error) {
	return s.inspect()
}

// Audit checks every configured contract without mutating files or calling providers.
func (s *Service) Audit() (Report, error) {
	return s.inspect()
}

// BumpPlan describes a planned replacement for one reference contract.
type BumpPlan struct {
	ContractID  string
	Path        string
	Expected    string
	Replacement string
	Before      string
	After       string
}

// PlanBump validates and plans a reference-contract replacement without writing files.
func (s *Service) PlanBump(contractID string) (BumpPlan, error) {
	contract, ok := s.reference(contractID)
	if !ok {
		return BumpPlan{}, fmt.Errorf("reference contract %q not found", contractID)
	}
	if contract.Replacement == "" {
		return BumpPlan{}, fmt.Errorf("reference contract %q replacement is absent", contractID)
	}
	path, err := s.contractPath(contract.File)
	if err != nil {
		return BumpPlan{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return BumpPlan{}, fmt.Errorf("read reference contract %q: %w", contractID, err)
	}
	count := strings.Count(string(content), contract.Expected)
	if count == 0 {
		return BumpPlan{}, fmt.Errorf("reference contract %q expected literal is stale or absent", contractID)
	}
	if count != 1 {
		return BumpPlan{}, fmt.Errorf("reference contract %q expected literal is ambiguous: found %d occurrences", contractID, count)
	}
	return BumpPlan{
		ContractID:  contractID,
		Path:        path,
		Expected:    contract.Expected,
		Replacement: contract.Replacement,
		Before:      string(content),
		After:       strings.Replace(string(content), contract.Expected, contract.Replacement, 1),
	}, nil
}

// Apply applies a previously planned reference bump only when apply is explicitly true.
func (s *Service) Apply(plan BumpPlan, apply bool) error {
	if !apply {
		return fmt.Errorf("apply requires explicit approval")
	}
	contract, ok := s.reference(plan.ContractID)
	if !ok {
		return fmt.Errorf("reference contract %q not found", plan.ContractID)
	}
	path, err := s.contractPath(contract.File)
	if err != nil {
		return err
	}
	if path != plan.Path || contract.Expected != plan.Expected || contract.Replacement != plan.Replacement {
		return fmt.Errorf("reference contract %q plan is stale", plan.ContractID)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read reference contract %q: %w", plan.ContractID, err)
	}
	current := string(content)
	if current != plan.Before {
		return fmt.Errorf("reference contract %q plan is stale", plan.ContractID)
	}
	if count := strings.Count(current, contract.Expected); count != 1 {
		return fmt.Errorf("reference contract %q expected literal is not an exact match: found %d occurrences", plan.ContractID, count)
	}
	return atomicWrite(path, []byte(plan.After), fileMode(path))
}

func (s *Service) inspect() (Report, error) {
	findings := make([]Finding, 0)
	for _, contract := range s.contracts.Reference {
		findings = append(findings, s.checkFile("reference", contract, "")...)
	}
	for _, contract := range s.contracts.MigrationCoverage {
		if _, err := s.contractPath(contract.Source); err != nil {
			findings = append(findings, Finding{ContractID: contract.ID, Type: "migration-coverage", Severity: contractSeverity(contract.Severity), Message: "source path escapes workspace", Path: contract.Source})
		} else if _, err := os.Stat(mustPath(s.contractPath(contract.Source))); err != nil && os.IsNotExist(err) {
			findings = append(findings, Finding{ContractID: contract.ID, Type: "migration-coverage", Severity: contractSeverity(contract.Severity), Message: "source file does not exist", Path: contract.Source})
		}
		findings = append(findings, s.checkFile("migration-coverage", contract, "")...)
	}
	for _, contract := range s.contracts.Artifact {
		findings = append(findings, s.checkFile("artifact", contract, "")...)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity == SeverityError
		}
		if findings[i].ContractID != findings[j].ContractID {
			return findings[i].ContractID < findings[j].ContractID
		}
		if findings[i].Type != findings[j].Type {
			return findings[i].Type < findings[j].Type
		}
		return false
	})
	return Report{Findings: findings}, nil
}

func (s *Service) checkFile(kind string, contract config.Contract, _ string) []Finding {
	path, err := s.contractPath(contract.File)
	if err != nil {
		return []Finding{{ContractID: contract.ID, Type: kind, Severity: contractSeverity(contract.Severity), Message: "path escapes workspace", Path: contract.File}}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Finding{{ContractID: contract.ID, Type: kind, Severity: contractSeverity(contract.Severity), Message: "file does not exist", Path: contract.File}}
		}
		return []Finding{{ContractID: contract.ID, Type: kind, Severity: contractSeverity(contract.Severity), Message: "file cannot be read", Path: contract.File}}
	}
	if kind == "artifact" && contract.Expected == "present" {
		return nil
	}
	if !strings.Contains(string(content), contract.Expected) {
		message := "file does not contain expected text"
		if kind == "migration-coverage" {
			message = "file does not contain expected migration marker"
		}
		return []Finding{{ContractID: contract.ID, Type: kind, Severity: contractSeverity(contract.Severity), Message: message, Path: contract.File}}
	}
	return nil
}

func (s *Service) reference(id string) (config.Contract, bool) {
	for _, contract := range s.contracts.Reference {
		if contract.ID == id {
			return contract, true
		}
	}
	return config.Contract{}, false
}

func (s *Service) contractPath(name string) (string, error) {
	if name == "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("contract path %q must remain inside workspace", name)
	}
	clean := filepath.Clean(name)
	path := filepath.Join(s.root, clean)
	if !inside(s.root, path) {
		return "", fmt.Errorf("contract path %q escapes workspace", name)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && !inside(s.root, resolved) {
		return "", fmt.Errorf("contract path %q escapes workspace through symlink", name)
	}
	parent := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil && !inside(s.root, resolved) {
		return "", fmt.Errorf("contract path %q escapes workspace through symlink", name)
	}
	return path, nil
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func contractSeverity(severity string) Severity {
	if severity == string(SeverityWarn) {
		return SeverityWarn
	}
	return SeverityError
}

func fileMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return 0o644
	}
	return info.Mode().Perm()
}

func atomicWrite(path string, content []byte, mode os.FileMode) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".smt-contract-*")
	if err != nil {
		return fmt.Errorf("create atomic contract file: %w", err)
	}
	tempName := temp.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temp.Close(); err == nil && closeErr != nil {
				err = fmt.Errorf("close atomic contract file: %w", closeErr)
			}
		}
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(mode); err != nil {
		return fmt.Errorf("set atomic contract file mode: %w", err)
	}
	if _, err := temp.Write(content); err != nil {
		return fmt.Errorf("write atomic contract file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync atomic contract file: %w", err)
	}
	if err := temp.Close(); err != nil {
		closed = true
		return fmt.Errorf("close atomic contract file: %w", err)
	}
	closed = true
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace contract file: %w", err)
	}
	return nil
}

func mustPath(path string, err error) string {
	if err != nil {
		return ""
	}
	return path
}
