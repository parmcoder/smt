package provider

import (
	"context"
	"net/http"
	"net/url"
)

// GitHub implements the provider-neutral contract with GitHub REST endpoints.
type GitHub struct{ client }

func NewGitHub(baseURL, token string, httpClient *http.Client) (*GitHub, error) {
	client, err := newClient("github", baseURL, token, httpClient)
	if err != nil {
		return nil, err
	}
	return &GitHub{client: client}, nil
}

type githubProjectResponse struct {
	FullName      string `json:"full_name"`
	SSHURL        string `json:"ssh_url"`
	HTMLURL       string `json:"html_url"`
	DefaultBranch string `json:"default_branch"`
}

func (g *GitHub) InspectProject(ctx context.Context, project string) (ProjectInfo, error) {
	parts, err := splitProject("github", project, 2)
	if err != nil {
		return ProjectInfo{}, err
	}
	response, err := g.request(ctx, http.MethodGet, "repos/"+url.PathEscape(parts[0])+"/"+url.PathEscape(parts[1]), nil)
	if err != nil {
		return ProjectInfo{}, err
	}
	if missing, err := inspectNotFound("github", response); missing || err != nil {
		if err != nil {
			return ProjectInfo{}, err
		}
		return ProjectInfo{Project: project}, nil
	}
	var value githubProjectResponse
	if err := decodeProviderResponse("github", response, &value); err != nil {
		return ProjectInfo{}, err
	}
	if value.FullName != project || value.SSHURL == "" || value.HTMLURL == "" {
		return ProjectInfo{}, errProviderResponse
	}
	return ProjectInfo{Exists: true, Project: value.FullName, SSHURL: value.SSHURL, WebURL: value.HTMLURL, DefaultBranch: value.DefaultBranch}, nil
}

func (g *GitHub) CreateProject(ctx context.Context, spec ProjectSpec) (ProjectInfo, error) {
	parts, err := splitProject("github", spec.Project, 2)
	if err != nil {
		return ProjectInfo{}, err
	}
	response, err := g.request(ctx, http.MethodPost, "orgs/"+url.PathEscape(parts[0])+"/repos", map[string]any{
		"name":    parts[1],
		"private": spec.Visibility != "public",
	})
	if err != nil {
		return ProjectInfo{}, err
	}
	var value githubProjectResponse
	if err := decodeProviderResponse("github", response, &value); err != nil {
		return ProjectInfo{}, err
	}
	if value.FullName != spec.Project || value.SSHURL == "" || value.HTMLURL == "" {
		return ProjectInfo{}, errProviderResponse
	}
	return ProjectInfo{Exists: true, Project: value.FullName, SSHURL: value.SSHURL, WebURL: value.HTMLURL, DefaultBranch: value.DefaultBranch}, nil
}
