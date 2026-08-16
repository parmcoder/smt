package config

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// ModuleCatalogSchemaVersion is the schema version for the embedded module catalog.
const ModuleCatalogSchemaVersion = 1

// ModuleCatalog is the reviewed, code-owned set of module declarations.
// It is intentionally not a Config field and is never loaded from YAML.
type ModuleCatalog struct {
	SchemaVersion int
	Modules       []ModuleDefinition
}

// ModuleDefinition declares one module's capabilities and reviewed metadata.
type ModuleDefinition struct {
	ID             string
	Category       string
	Layer          string
	Provides       []string
	Requires       []string
	Optional       []string
	Repository     ModuleRepositoryPlacement
	Agents         []string
	Skills         []string
	Verification   []VerificationRequirement
	ScaffoldAssets []ScaffoldAsset
}

// ModuleRepositoryPlacement is the safe default repository location for a module.
type ModuleRepositoryPlacement struct {
	Path  string
	Scope string
}

// VerificationRequirement is a declaration only. SMT never executes Argv.
type VerificationRequirement struct {
	ID              string
	Argv            []string
	MutatesWorktree bool
}

// ScaffoldAsset identifies a reviewed, checked-in declaration or manifest.
type ScaffoldAsset struct {
	ID       string
	Path     string
	Revision string
	Version  string
}

var moduleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var moduleCategoryLayers = map[string]string{
	"application":    "application-components",
	"control-plane":  "control-plane",
	"infrastructure": "shared-infrastructure",
	"quality":        "quality-verification",
	"platform":       "platform-delivery",
}

// BuiltInModuleCatalog returns a deep copy so callers cannot mutate SMT's catalog.
func BuiltInModuleCatalog() ModuleCatalog { return cloneModuleCatalog(builtInModuleCatalog) }

// ModuleCatalogDefinitions returns a safe copy of the built-in definitions.
func ModuleCatalogDefinitions() []ModuleDefinition { return BuiltInModuleCatalog().Modules }

// ValidateModuleCatalog validates a code-owned catalog without executing any declaration.
func ValidateModuleCatalog(catalog ModuleCatalog) error { return catalog.Validate() }

// QualityRootModule returns the unique quality declaration intended to remain
// in the root repository. Catalog consumers use its role and placement rather
// than coupling to a module identifier.
func QualityRootModule(catalog ModuleCatalog) (ModuleDefinition, error) {
	if err := catalog.Validate(); err != nil {
		return ModuleDefinition{}, fmt.Errorf("quality root module catalog is invalid: %w", err)
	}
	var matches []ModuleDefinition
	for _, module := range catalog.Modules {
		if module.Category == "quality" && module.Layer == "quality-verification" && module.Repository.Path == "." && module.Repository.Scope == "repo" {
			matches = append(matches, module)
		}
	}
	if len(matches) == 0 {
		return ModuleDefinition{}, fmt.Errorf("quality root module is missing from the catalog")
	}
	if len(matches) != 1 {
		return ModuleDefinition{}, fmt.Errorf("quality root module is ambiguous in the catalog")
	}
	return matches[0], nil
}

func (c *Config) validateRepositoryModules() error {
	definitions := BuiltInModuleCatalog().Modules
	byID := make(map[string]ModuleDefinition, len(definitions))
	for _, definition := range definitions {
		byID[definition.ID] = definition
	}
	selected := make(map[string]struct{})
	for _, repository := range c.Repositories {
		seen := make(map[string]struct{}, len(repository.Modules))
		for _, moduleID := range repository.Modules {
			if _, exists := byID[moduleID]; !exists {
				return fmt.Errorf("repository %q references unknown module %q", repository.ID, moduleID)
			}
			if _, exists := seen[moduleID]; exists {
				return fmt.Errorf("repository %q has duplicate module %q", repository.ID, moduleID)
			}
			seen[moduleID] = struct{}{}
			selected[moduleID] = struct{}{}
		}
	}
	provided := make(map[string]struct{})
	for moduleID := range selected {
		for _, capability := range byID[moduleID].Provides {
			provided[capability] = struct{}{}
		}
	}
	for moduleID := range selected {
		for _, capability := range byID[moduleID].Requires {
			if _, exists := provided[capability]; !exists {
				return fmt.Errorf("selected module %q requires capability %q", moduleID, capability)
			}
		}
	}
	return nil
}

// Validate checks the catalog schema, references, placement safety, and dependency graph.
func (catalog ModuleCatalog) Validate() error {
	if catalog.SchemaVersion != ModuleCatalogSchemaVersion {
		return fmt.Errorf("module catalog schema version must be %d", ModuleCatalogSchemaVersion)
	}
	if len(catalog.Modules) == 0 {
		return fmt.Errorf("module catalog must contain at least one module")
	}
	seenIDs := make(map[string]struct{}, len(catalog.Modules))
	providers := make(map[string]string)
	for i, module := range catalog.Modules {
		if !moduleIDPattern.MatchString(module.ID) {
			return fmt.Errorf("module %d has invalid ID %q", i, module.ID)
		}
		if _, exists := seenIDs[module.ID]; exists {
			return fmt.Errorf("module catalog has duplicate module ID %q", module.ID)
		}
		seenIDs[module.ID] = struct{}{}
		if wantLayer, ok := moduleCategoryLayers[module.Category]; !ok || module.Layer != wantLayer {
			return fmt.Errorf("module %q has unsupported category/layer %q/%q", module.ID, module.Category, module.Layer)
		}
		if err := validateCapabilityList(module.ID, "provides", module.Provides); err != nil {
			return err
		}
		if err := validateCapabilityList(module.ID, "requires", module.Requires); err != nil {
			return err
		}
		if err := validateCapabilityList(module.ID, "optional", module.Optional); err != nil {
			return err
		}
		if err := validatePlacement(module.ID, module.Repository); err != nil {
			return err
		}
		if err := validateReferences(module.ID, "agent", module.Agents); err != nil {
			return err
		}
		if err := validateReferences(module.ID, "skill", module.Skills); err != nil {
			return err
		}
		if err := validateVerifications(module); err != nil {
			return err
		}
		if err := validateScaffoldAssets(module); err != nil {
			return err
		}
		for _, capability := range module.Provides {
			if provider, exists := providers[capability]; exists {
				return fmt.Errorf("module capability %q is provided by both %q and %q", capability, provider, module.ID)
			}
			providers[capability] = module.ID
		}
	}

	dependencies := make(map[string][]string, len(catalog.Modules))
	for _, module := range catalog.Modules {
		for _, capability := range append(append([]string(nil), module.Requires...), module.Optional...) {
			if _, exists := providers[capability]; !exists {
				return fmt.Errorf("module %q references unknown capability %q", module.ID, capability)
			}
		}
		for _, capability := range module.Requires {
			provider := providers[capability]
			if contains(dependencies[module.ID], provider) {
				continue
			}
			dependencies[module.ID] = append(dependencies[module.ID], provider)
		}
	}
	if cycle := moduleDependencyCycle(dependencies); cycle != "" {
		return fmt.Errorf("module catalog dependency cycle: %s", cycle)
	}
	return nil
}

func validateCapabilityList(moduleID, field string, capabilities []string) error {
	seen := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if !moduleIDPattern.MatchString(capability) {
			return fmt.Errorf("module %q has invalid %s capability %q", moduleID, field, capability)
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("module %q has duplicate %s capability %q", moduleID, field, capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

func validatePlacement(moduleID string, placement ModuleRepositoryPlacement) error {
	if !moduleIDPattern.MatchString(placement.Scope) {
		return fmt.Errorf("module %q repository scope must be a safe identifier", moduleID)
	}
	if err := validateSafeRelativePath(placement.Path); err != nil {
		return fmt.Errorf("module %q repository path: %w", moduleID, err)
	}
	return nil
}

func validateSafeRelativePath(value string) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("path must be a non-empty safe relative path")
	}
	if filepath.IsAbs(value) || path.IsAbs(value) || filepath.VolumeName(value) != "" || (len(value) >= 3 && value[1] == ':' && (value[2] == '/' || value[2] == '\\')) {
		return fmt.Errorf("path %q must be relative", value)
	}
	clean := path.Clean(strings.ReplaceAll(value, `\`, "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("path %q must not traverse outside its root", value)
	}
	return nil
}

func validateReferences(moduleID, kind string, references []string) error {
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if strings.TrimSpace(reference) == "" || strings.ContainsAny(reference, "\r\n\t ") {
			return fmt.Errorf("module %q has invalid %s reference %q", moduleID, kind, reference)
		}
		if _, exists := seen[reference]; exists {
			return fmt.Errorf("module %q has duplicate %s reference %q", moduleID, kind, reference)
		}
		seen[reference] = struct{}{}
	}
	return nil
}

func validateVerifications(module ModuleDefinition) error {
	seen := make(map[string]struct{}, len(module.Verification))
	for _, verification := range module.Verification {
		if strings.TrimSpace(verification.ID) == "" {
			return fmt.Errorf("module %q verification ID must not be empty", module.ID)
		}
		if _, exists := seen[verification.ID]; exists {
			return fmt.Errorf("module %q has duplicate verification ID %q", module.ID, verification.ID)
		}
		seen[verification.ID] = struct{}{}
		if len(verification.Argv) == 0 {
			return fmt.Errorf("module %q verification %q argv must not be empty", module.ID, verification.ID)
		}
		for _, arg := range verification.Argv {
			if arg == "" {
				return fmt.Errorf("module %q verification %q argv contains an empty argument", module.ID, verification.ID)
			}
		}
	}
	return nil
}

func validateScaffoldAssets(module ModuleDefinition) error {
	seen := make(map[string]struct{}, len(module.ScaffoldAssets))
	for _, asset := range module.ScaffoldAssets {
		if strings.TrimSpace(asset.ID) == "" {
			return fmt.Errorf("module %q scaffold asset ID must not be empty", module.ID)
		}
		if _, exists := seen[asset.ID]; exists {
			return fmt.Errorf("module %q has duplicate scaffold asset ID %q", module.ID, asset.ID)
		}
		seen[asset.ID] = struct{}{}
		if err := validateSafeRelativePath(asset.Path); err != nil {
			return fmt.Errorf("module %q scaffold asset %q path: %w", module.ID, asset.ID, err)
		}
		if strings.TrimSpace(asset.Revision) == "" && strings.TrimSpace(asset.Version) == "" {
			return fmt.Errorf("module %q scaffold asset %q requires a revision or version", module.ID, asset.ID)
		}
	}
	return nil
}

func moduleDependencyCycle(dependencies map[string][]string) string {
	state := make(map[string]uint8, len(dependencies))
	stack := make([]string, 0, len(dependencies))
	var visit func(string) string
	visit = func(module string) string {
		switch state[module] {
		case 1:
			for i := range stack {
				if stack[i] == module {
					return strings.Join(append(stack[i:], module), " -> ")
				}
			}
			return module
		case 2:
			return ""
		}
		state[module] = 1
		stack = append(stack, module)
		for _, dependency := range dependencies[module] {
			if cycle := visit(dependency); cycle != "" {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		state[module] = 2
		return ""
	}
	for module := range dependencies {
		if cycle := visit(module); cycle != "" {
			return cycle
		}
	}
	return ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cloneModuleCatalog(catalog ModuleCatalog) ModuleCatalog {
	clone := ModuleCatalog{SchemaVersion: catalog.SchemaVersion, Modules: make([]ModuleDefinition, len(catalog.Modules))}
	for i, module := range catalog.Modules {
		clone.Modules[i] = module
		clone.Modules[i].Provides = append([]string(nil), module.Provides...)
		clone.Modules[i].Requires = append([]string(nil), module.Requires...)
		clone.Modules[i].Optional = append([]string(nil), module.Optional...)
		clone.Modules[i].Agents = append([]string(nil), module.Agents...)
		clone.Modules[i].Skills = append([]string(nil), module.Skills...)
		clone.Modules[i].Verification = append([]VerificationRequirement(nil), module.Verification...)
		for j := range clone.Modules[i].Verification {
			clone.Modules[i].Verification[j].Argv = append([]string(nil), module.Verification[j].Argv...)
		}
		clone.Modules[i].ScaffoldAssets = append([]ScaffoldAsset(nil), module.ScaffoldAssets...)
	}
	return clone
}

var builtInModuleCatalog = ModuleCatalog{
	SchemaVersion: ModuleCatalogSchemaVersion,
	Modules: []ModuleDefinition{
		{
			ID: "web", Category: "application", Layer: "application-components", Provides: []string{"web"},
			Repository: ModuleRepositoryPlacement{Path: "web-app", Scope: "web"}, Agents: []string{"web_worker"},
			Skills:         []string{"build-web-apps:react-best-practices", "build-web-apps:frontend-testing-debugging"},
			Verification:   []VerificationRequirement{{ID: "web-verify", Argv: []string{"npm", "run", "verify"}, MutatesWorktree: false}},
			ScaffoldAssets: []ScaffoldAsset{{ID: "web-manifest", Path: "package.json", Revision: "nextjs-16.2.9", Version: "v1"}},
		},
		{
			ID: "mobile", Category: "application", Layer: "application-components", Provides: []string{"mobile"},
			Repository: ModuleRepositoryPlacement{Path: "mobile-app", Scope: "mobile"}, Agents: []string{"mobile_worker"},
			Skills:         []string{"flutter-apply-architecture-best-practices", "flutter-add-integration-test"},
			Verification:   []VerificationRequirement{{ID: "mobile-verify", Argv: []string{"flutter", "analyze"}, MutatesWorktree: false}},
			ScaffoldAssets: []ScaffoldAsset{{ID: "mobile-manifest", Path: "pubspec.yaml", Revision: "flutter-3.44.9", Version: "v1"}},
		},
		{
			ID: "api", Category: "application", Layer: "application-components", Provides: []string{"api"},
			Repository: ModuleRepositoryPlacement{Path: "apis", Scope: "api"}, Agents: []string{"api_worker"},
			Skills:         []string{"godex:godex-go-backend"},
			Verification:   []VerificationRequirement{{ID: "api-verify", Argv: []string{"go", "test", "./..."}, MutatesWorktree: false}},
			ScaffoldAssets: []ScaffoldAsset{{ID: "api-manifest", Path: "go.mod", Revision: "go-1.26.5", Version: "v1"}},
		},
		{
			ID: "database", Category: "infrastructure", Layer: "shared-infrastructure", Provides: []string{"database"},
			Repository: ModuleRepositoryPlacement{Path: "database", Scope: "database"}, Agents: []string{"database_worker"},
			Skills:         []string{"godex:godex-go-backend"},
			Verification:   []VerificationRequirement{{ID: "database-verify", Argv: []string{"pg_isready"}, MutatesWorktree: false}},
			ScaffoldAssets: []ScaffoldAsset{{ID: "database-declaration", Path: "migrations", Revision: "postgresql-18", Version: "v1"}},
		},
		{
			ID: "e2e", Category: "quality", Layer: "quality-verification", Provides: []string{"e2e"},
			Optional: []string{"web", "api", "mobile"}, Repository: ModuleRepositoryPlacement{Path: ".", Scope: "repo"},
			Agents: []string{"e2e_worker"}, Skills: []string{"build-web-apps:frontend-testing-debugging"},
			Verification:   []VerificationRequirement{{ID: "e2e-verify", Argv: []string{"task", "verify"}, MutatesWorktree: false}},
			ScaffoldAssets: []ScaffoldAsset{{ID: "e2e-declaration", Path: "e2e", Revision: "v1", Version: "v1"}},
		},
	},
}
