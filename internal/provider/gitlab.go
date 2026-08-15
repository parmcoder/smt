package provider

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// GitLab implements the provider-neutral contract with GitLab REST endpoints.
type GitLab struct{ client }

func NewGitLab(baseURL, token string, httpClient *http.Client) (*GitLab, error) {
	client, err := newClient("gitlab", baseURL, token, httpClient)
	if err != nil {
		return nil, err
	}
	return &GitLab{client: client}, nil
}

type gitlabProjectResponse struct {
	ID                int    `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	SSHURLToRepo      string `json:"ssh_url_to_repo"`
	WebURL            string `json:"web_url"`
	DefaultBranch     string `json:"default_branch"`
}

func (g *GitLab) InspectProject(ctx context.Context, project string) (ProjectInfo, error) {
	if _, err := splitProject("gitlab", project, 0); err != nil {
		return ProjectInfo{}, err
	}
	response, err := g.request(ctx, http.MethodGet, "projects/"+url.PathEscape(project), nil)
	if err != nil {
		return ProjectInfo{}, err
	}
	if missing, err := inspectNotFound("gitlab", response); missing || err != nil {
		if err != nil {
			return ProjectInfo{}, err
		}
		return ProjectInfo{Project: project}, nil
	}
	var value gitlabProjectResponse
	if err := decodeProviderResponse("gitlab", response, &value); err != nil {
		return ProjectInfo{}, err
	}
	if value.PathWithNamespace != project || value.SSHURLToRepo == "" || value.WebURL == "" {
		return ProjectInfo{}, errProviderResponse
	}
	return ProjectInfo{Exists: true, Project: value.PathWithNamespace, SSHURL: value.SSHURLToRepo, WebURL: value.WebURL, DefaultBranch: value.DefaultBranch}, nil
}

type gitlabNamespaceResponse struct {
	ID       int    `json:"id"`
	FullPath string `json:"full_path"`
}

func (g *GitLab) CreateProject(ctx context.Context, spec ProjectSpec) (ProjectInfo, error) {
	parts, err := splitProject("gitlab", spec.Project, 0)
	if err != nil {
		return ProjectInfo{}, err
	}
	namespace := strings.Join(parts[:len(parts)-1], "/")
	response, err := g.request(ctx, http.MethodGet, "groups/"+url.PathEscape(namespace), nil)
	if err != nil {
		return ProjectInfo{}, err
	}
	var group gitlabNamespaceResponse
	if err := decodeProviderResponse("gitlab", response, &group); err != nil {
		return ProjectInfo{}, err
	}
	if group.ID == 0 || group.FullPath != namespace {
		return ProjectInfo{}, errProviderResponse
	}
	response, err = g.request(ctx, http.MethodPost, "projects", map[string]any{
		"name": parts[len(parts)-1], "path": parts[len(parts)-1], "namespace_id": group.ID,
		"visibility": spec.Visibility, "initialize_with_readme": false,
	})
	if err != nil {
		return ProjectInfo{}, err
	}
	var value gitlabProjectResponse
	if err := decodeProviderResponse("gitlab", response, &value); err != nil {
		return ProjectInfo{}, err
	}
	if value.PathWithNamespace != spec.Project || value.SSHURLToRepo == "" || value.WebURL == "" {
		return ProjectInfo{}, errProviderResponse
	}
	return ProjectInfo{Exists: true, Project: value.PathWithNamespace, SSHURL: value.SSHURLToRepo, WebURL: value.WebURL, DefaultBranch: value.DefaultBranch}, nil
}
