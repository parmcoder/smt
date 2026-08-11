package provider

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
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
	return ProjectInfo{Exists: true, Project: value.PathWithNamespace, SSHURL: value.SSHURLToRepo, WebURL: value.WebURL}, nil
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
	return ProjectInfo{Exists: true, Project: value.PathWithNamespace, SSHURL: value.SSHURLToRepo, WebURL: value.WebURL}, nil
}

type gitlabReviewResponse struct {
	IID          int    `json:"iid"`
	WebURL       string `json:"web_url"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Draft        bool   `json:"draft"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

func (g *GitLab) FindOpenReviews(ctx context.Context, spec ReviewSpec) ([]ReviewInfo, error) {
	if _, err := splitProject("gitlab", spec.Project, 0); err != nil {
		return nil, err
	}
	endpoint := "projects/" + url.PathEscape(spec.Project) + "/merge_requests?state=opened&source_branch=" + url.QueryEscape(spec.SourceBranch) + "&target_branch=" + url.QueryEscape(spec.TargetBranch)
	response, err := g.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var values []gitlabReviewResponse
	if err := decodeProviderResponse("gitlab", response, &values); err != nil {
		return nil, err
	}
	result := make([]ReviewInfo, 0, len(values))
	for _, value := range values {
		if value.SourceBranch == spec.SourceBranch && value.TargetBranch == spec.TargetBranch {
			result = append(result, gitlabReview(value, spec.Project))
		}
	}
	return result, nil
}

func (g *GitLab) CreateReview(ctx context.Context, spec ReviewSpec) (ReviewInfo, error) {
	if _, err := splitProject("gitlab", spec.Project, 0); err != nil {
		return ReviewInfo{}, err
	}
	response, err := g.request(ctx, http.MethodPost, "projects/"+url.PathEscape(spec.Project)+"/merge_requests", map[string]any{
		"source_branch": spec.SourceBranch, "target_branch": spec.TargetBranch, "title": spec.Title,
		"description": spec.Description, "draft": spec.Draft,
	})
	if err != nil {
		return ReviewInfo{}, err
	}
	var value gitlabReviewResponse
	if err := decodeProviderResponse("gitlab", response, &value); err != nil {
		return ReviewInfo{}, err
	}
	return gitlabReview(value, spec.Project), nil
}

func (g *GitLab) SetReady(ctx context.Context, review ReviewInfo) (ReviewInfo, error) {
	if _, err := splitProject("gitlab", review.Project, 0); err != nil {
		return ReviewInfo{}, err
	}
	response, err := g.request(ctx, http.MethodPut, "projects/"+url.PathEscape(review.Project)+"/merge_requests/"+url.PathEscape(review.ID), map[string]any{"draft": false})
	if err != nil {
		return ReviewInfo{}, err
	}
	var value gitlabReviewResponse
	if err := decodeProviderResponse("gitlab", response, &value); err != nil {
		return ReviewInfo{}, err
	}
	return gitlabReview(value, review.Project), nil
}

func gitlabReview(value gitlabReviewResponse, project string) ReviewInfo {
	return ReviewInfo{Project: project, ID: strconv.Itoa(value.IID), URL: value.WebURL, Title: value.Title, Description: value.Description, SourceBranch: value.SourceBranch, TargetBranch: value.TargetBranch, Draft: value.Draft}
}
