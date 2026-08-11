package submission

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/parmcoder/smt/internal/config"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
)

// ReviewContent derives stable provider title/body content from the manifest.
func ReviewContent(manifest workspacepkg.RunManifest, repositoryID string, childLinks map[string]string) (string, string, error) {
	var repository workspacepkg.ManifestRepository
	found := false
	for _, candidate := range manifest.Repositories {
		if candidate.ID == repositoryID {
			repository = candidate
			found = true
			break
		}
	}
	if !found {
		return "", "", fmt.Errorf("review repository %s is not in the prepared manifest", repositoryID)
	}
	title := manifest.Feature.Title
	if len(repository.Tasks) == 1 {
		title = repository.Tasks[0].Title
	}
	if len(repository.Tasks) > 1 {
		title = manifest.Feature.Title + " — " + repositoryID
	}
	if strings.TrimSpace(title) == "" {
		title = manifest.Feature.ID + " — " + repositoryID
	}
	var body strings.Builder
	body.WriteString("## Summary\n\n")
	if len(repository.Tasks) == 0 {
		body.WriteString(strings.TrimSpace(manifest.Feature.Description))
	} else {
		for index, task := range repository.Tasks {
			if index > 0 {
				body.WriteString("\n\n")
			}
			body.WriteString(strings.TrimSpace(task.Description))
		}
	}
	body.WriteString("\n\n## Acceptance criteria\n\n")
	if len(repository.Tasks) == 0 {
		body.WriteString(strings.TrimSpace(manifest.Feature.AcceptanceCriteria))
	} else {
		for index, task := range repository.Tasks {
			if index > 0 {
				body.WriteString("\n\n")
			}
			body.WriteString(strings.TrimSpace(task.AcceptanceCriteria))
		}
	}
	body.WriteString("\n\n## Work items\n\n")
	seen := make(map[string]struct{})
	addReference := func(reference string) {
		if reference == "" {
			return
		}
		if _, ok := seen[reference]; ok {
			return
		}
		seen[reference] = struct{}{}
		fmt.Fprintf(&body, "Closes `%s`\n", reference)
	}
	if len(repository.Tasks) == 0 {
		addReference(manifest.Feature.ID)
	} else {
		for _, task := range repository.Tasks {
			addReference(task.ID)
			addReference(task.ExternalRef)
		}
	}
	if len(childLinks) > 0 {
		body.WriteString("\n## Related repositories\n\n")
		for _, childID := range sortedKeys(childLinks) {
			fmt.Fprintf(&body, "- %s: %s\n", childID, childLinks[childID])
		}
	}
	return title, strings.TrimSpace(body.String()) + "\n", nil
}

// ReviewLink is a copy-ready provider web link for manual review creation.
func ReviewLink(providerName string, settings config.ProviderConfig, project, sourceBranch, targetBranch string) string {
	base := settings.APIBaseURL
	if base == "" {
		base = settings.EnterpriseBaseURL
	}
	if base == "" {
		if providerName == "gitlab" {
			base = "https://gitlab.com/"
		} else {
			base = "https://github.com/"
		}
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if providerName == "github" {
		if parsed.Host == "api.github.com" {
			parsed.Host = "github.com"
		}
		parsed.Path = strings.TrimSuffix(parsed.Path, "/api/v3") + "/" + project + "/compare/" + url.PathEscape(targetBranch) + "..." + url.PathEscape(sourceBranch)
		return parsed.String()
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/api/v4") + "/" + project + "/-/merge_requests/new"
	query := parsed.Query()
	query.Set("merge_request[source_branch]", sourceBranch)
	query.Set("merge_request[target_branch]", targetBranch)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
