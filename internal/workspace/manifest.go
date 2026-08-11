package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/parmcoder/smt/internal/config"
)

var manifestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// BaseState records the source branch and commit captured before preparation.
type BaseState struct {
	Branch string `json:"base_branch"`
	Commit string `json:"base_commit"`
}

// ManifestRepository is one configured repository in a prepared run.
type ManifestRepository struct {
	ID              string           `json:"id"`
	Path            string           `json:"path"`
	BaseBranch      string           `json:"base_branch"`
	BaseCommit      string           `json:"base_commit"`
	Ownership       string           `json:"ownership"`
	CheckProfiles   []string         `json:"check_profiles,omitempty"`
	IntegrationGate string           `json:"integration_gate"`
	Tasks           []TaskAssignment `json:"tasks,omitempty"`
}

// RunManifest is the secret-free authority for one prepared feature workspace.
type RunManifest struct {
	SchemaVersion int                  `json:"schema_version"`
	Feature       FeatureContext       `json:"feature"`
	WorkspacePath string               `json:"workspace_path"`
	Branch        string               `json:"branch"`
	Repositories  []ManifestRepository `json:"repositories"`
}

// BuildRunManifest combines resolved tasks with the captured Git base state.
func BuildRunManifest(assignments FeatureAssignments, cfg config.Config, workspacePath, branch string, bases map[string]BaseState) (RunManifest, error) {
	if !manifestIDPattern.MatchString(assignments.Feature.ID) {
		return RunManifest{}, errors.New("manifest feature ID is invalid")
	}
	if strings.TrimSpace(workspacePath) == "" || strings.TrimSpace(branch) == "" {
		return RunManifest{}, errors.New("manifest workspace path and branch are required")
	}
	absolutePath, err := filepath.Abs(workspacePath)
	if err != nil {
		return RunManifest{}, errors.New("manifest workspace path is invalid")
	}
	byRepository := make(map[string][]TaskAssignment, len(assignments.Repositories))
	for _, repository := range assignments.Repositories {
		byRepository[repository.Repository.ID] = append([]TaskAssignment(nil), repository.Tasks...)
	}
	manifest := RunManifest{
		SchemaVersion: 1,
		Feature:       assignments.Feature,
		WorkspacePath: absolutePath,
		Branch:        branch,
		Repositories:  make([]ManifestRepository, 0, len(cfg.Repositories)),
	}
	for _, repository := range cfg.Repositories {
		base, ok := bases[repository.ID]
		if !ok || strings.TrimSpace(base.Branch) == "" || strings.TrimSpace(base.Commit) == "" {
			return RunManifest{}, fmt.Errorf("manifest base state is missing for repository %s", repository.ID)
		}
		manifest.Repositories = append(manifest.Repositories, ManifestRepository{
			ID:              repository.ID,
			Path:            repository.Path,
			BaseBranch:      base.Branch,
			BaseCommit:      base.Commit,
			Ownership:       manifestOwnership(repository),
			CheckProfiles:   manifestCheckProfiles(repository),
			IntegrationGate: manifestIntegrationGate(repository),
			Tasks:           byRepository[repository.ID],
		})
	}
	return manifest, nil
}

func manifestOwnership(repository config.Repository) string {
	if filepath.Clean(repository.Path) == "." {
		return "integration-worker"
	}
	return "repository-worker"
}

func manifestIntegrationGate(repository config.Repository) string {
	if filepath.Clean(repository.Path) == "." {
		return "root"
	}
	return "root-gitlink"
}

func manifestCheckProfiles(repository config.Repository) []string {
	profiles := repository.Profiles
	if profiles == nil && (repository.HasChecks || len(repository.Checks) > 0) {
		profiles = config.CheckProfiles{"legacy": repository.Checks}
	}
	if len(profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateManifestRepository confirms that the prepared metadata still
// matches the current repository configuration before later workspace work.
func ValidateManifestRepository(entry ManifestRepository, repository config.Repository) error {
	if entry.ID != repository.ID || filepath.Clean(entry.Path) != filepath.Clean(repository.Path) {
		return errors.New("prepared manifest metadata does not match configuration")
	}
	if entry.Ownership != manifestOwnership(repository) || entry.IntegrationGate != manifestIntegrationGate(repository) {
		return errors.New("prepared manifest metadata does not match configuration")
	}
	wantProfiles := manifestCheckProfiles(repository)
	if len(entry.CheckProfiles) != len(wantProfiles) {
		return errors.New("prepared manifest metadata does not match configuration")
	}
	for index := range wantProfiles {
		if entry.CheckProfiles[index] != wantProfiles[index] {
			return errors.New("prepared manifest metadata does not match configuration")
		}
	}
	return nil
}

// WriteRunManifest atomically writes the ignored run manifest after preparation.
func WriteRunManifest(workspacePath string, manifest RunManifest) (string, error) {
	if manifest.SchemaVersion != 1 || !manifestIDPattern.MatchString(manifest.Feature.ID) {
		return "", errors.New("manifest is invalid")
	}
	root, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", errors.New("manifest workspace path is invalid")
	}
	if filepath.Clean(manifest.WorkspacePath) != root {
		return "", errors.New("manifest workspace path does not match destination")
	}
	directory := filepath.Join(root, ".smt", "runs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", errors.New("create manifest directory")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", errors.New("encode manifest")
	}
	data = append(data, '\n')
	path := filepath.Join(directory, manifest.Feature.ID+".json")
	temporary, err := os.CreateTemp(directory, ".manifest-*.tmp")
	if err != nil {
		return "", errors.New("create manifest temporary file")
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", errors.New("protect manifest temporary file")
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", errors.New("write manifest")
	}
	if err := temporary.Close(); err != nil {
		return "", errors.New("close manifest temporary file")
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return "", errors.New("publish manifest")
	}
	return path, nil
}

// FindRunManifest loads exactly the manifest for a feature, workspace, and branch.
func FindRunManifest(workspacePath, featureID, branch string) (RunManifest, error) {
	if !manifestIDPattern.MatchString(featureID) || strings.TrimSpace(branch) == "" {
		return RunManifest{}, errors.New("prepared workspace manifest is invalid")
	}
	root, err := filepath.Abs(workspacePath)
	if err != nil {
		return RunManifest{}, errors.New("prepared workspace path is invalid")
	}
	path := filepath.Join(root, ".smt", "runs", featureID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RunManifest{}, errors.New("prepared workspace manifest was not found")
		}
		return RunManifest{}, errors.New("read prepared workspace manifest")
	}
	var manifest RunManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Feature.ID != featureID || manifest.Branch != branch || validateManifest(manifest, root) != nil {
		return RunManifest{}, errors.New("prepared workspace manifest is corrupt or does not match")
	}
	return manifest, nil
}

// FindPreparedRepository discovers the single run manifest matching the
// prepared workspace root, current repository path, and branch.
func FindPreparedRepository(workspacePath, currentPath, branch string) (RunManifest, ManifestRepository, error) {
	root, err := filepath.Abs(workspacePath)
	if err != nil || strings.TrimSpace(branch) == "" {
		return RunManifest{}, ManifestRepository{}, errors.New("prepared workspace path or branch is invalid")
	}
	current, err := filepath.Abs(currentPath)
	if err != nil {
		return RunManifest{}, ManifestRepository{}, errors.New("prepared repository path is invalid")
	}
	entries, err := os.ReadDir(filepath.Join(root, ".smt", "runs"))
	if err != nil {
		if os.IsNotExist(err) {
			return RunManifest{}, ManifestRepository{}, errors.New("prepared workspace manifest was not found")
		}
		return RunManifest{}, ManifestRepository{}, errors.New("read prepared workspace manifests")
	}
	var matches []struct {
		manifest   RunManifest
		repository ManifestRepository
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(root, ".smt", "runs", entry.Name()))
		if readErr != nil {
			return RunManifest{}, ManifestRepository{}, errors.New("read prepared workspace manifest")
		}
		var manifest RunManifest
		if json.Unmarshal(data, &manifest) != nil || validateManifest(manifest, root) != nil || manifest.Branch != branch {
			return RunManifest{}, ManifestRepository{}, errors.New("prepared workspace manifest is corrupt or does not match")
		}
		for _, repository := range manifest.Repositories {
			repositoryPath := filepath.Join(root, repository.Path)
			if filepath.Clean(repositoryPath) == filepath.Clean(current) {
				matches = append(matches, struct {
					manifest   RunManifest
					repository ManifestRepository
				}{manifest: manifest, repository: repository})
			}
		}
	}
	if len(matches) == 0 {
		return RunManifest{}, ManifestRepository{}, errors.New("prepared repository is not assigned by a matching manifest")
	}
	if len(matches) > 1 {
		return RunManifest{}, ManifestRepository{}, errors.New("multiple matching prepared workspace manifests were found")
	}
	return matches[0].manifest, matches[0].repository, nil
}

func validateManifest(manifest RunManifest, root string) error {
	if manifest.SchemaVersion != 1 || !manifestIDPattern.MatchString(manifest.Feature.ID) || strings.TrimSpace(manifest.Branch) == "" {
		return errors.New("manifest metadata is invalid")
	}
	workspacePath, err := filepath.Abs(manifest.WorkspacePath)
	if err != nil || filepath.Clean(workspacePath) != filepath.Clean(root) {
		return errors.New("manifest workspace path is invalid")
	}
	seenIDs := make(map[string]struct{}, len(manifest.Repositories))
	seenPaths := make(map[string]struct{}, len(manifest.Repositories))
	for _, repository := range manifest.Repositories {
		if strings.TrimSpace(repository.ID) == "" || strings.TrimSpace(repository.Path) == "" || strings.TrimSpace(repository.BaseBranch) == "" || strings.TrimSpace(repository.BaseCommit) == "" {
			return errors.New("manifest repository is invalid")
		}
		if _, exists := seenIDs[repository.ID]; exists {
			return errors.New("manifest contains duplicate repository IDs")
		}
		seenIDs[repository.ID] = struct{}{}
		cleanedPath := filepath.Clean(repository.Path)
		if filepath.IsAbs(repository.Path) {
			return errors.New("manifest repository path is invalid")
		}
		resolvedPath := filepath.Clean(filepath.Join(root, cleanedPath))
		if resolvedPath != filepath.Clean(root) && !strings.HasPrefix(resolvedPath, filepath.Clean(root)+string(filepath.Separator)) {
			return errors.New("manifest repository path is invalid")
		}
		if _, exists := seenPaths[cleanedPath]; exists {
			return errors.New("manifest contains duplicate repository paths")
		}
		seenPaths[cleanedPath] = struct{}{}
		if repository.Ownership != "repository-worker" && repository.Ownership != "integration-worker" {
			return errors.New("manifest repository ownership is invalid")
		}
		if repository.IntegrationGate != "root" && repository.IntegrationGate != "root-gitlink" {
			return errors.New("manifest integration gate is invalid")
		}
		wantOwnership := "repository-worker"
		wantGate := "root-gitlink"
		if cleanedPath == "." {
			wantOwnership = "integration-worker"
			wantGate = "root"
		}
		if repository.Ownership != wantOwnership || repository.IntegrationGate != wantGate {
			return errors.New("manifest repository ownership boundary is invalid")
		}
		if !sort.StringsAreSorted(repository.CheckProfiles) {
			return errors.New("manifest check profiles are not deterministic")
		}
		for index, profile := range repository.CheckProfiles {
			if strings.TrimSpace(profile) == "" || (index > 0 && profile == repository.CheckProfiles[index-1]) {
				return errors.New("manifest check profiles are invalid")
			}
		}
		for _, task := range repository.Tasks {
			if !manifestIDPattern.MatchString(task.ID) || (task.ExternalRef != "" && !externalReferencePattern.MatchString(task.ExternalRef)) {
				return errors.New("manifest task is invalid")
			}
			allowed := make(map[string]struct{}, len(task.AllowedReferences))
			for _, reference := range task.AllowedReferences {
				if strings.TrimSpace(reference) == "" {
					return errors.New("manifest task reference is invalid")
				}
				allowed[reference] = struct{}{}
			}
			if _, exists := allowed[task.ID]; !exists {
				return errors.New("manifest task references are incomplete")
			}
			if task.ExternalRef != "" {
				if _, exists := allowed[task.ExternalRef]; !exists {
					return errors.New("manifest task references are incomplete")
				}
			}
		}
	}
	return nil
}
