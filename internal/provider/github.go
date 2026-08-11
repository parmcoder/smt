package provider

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
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
	FullName string `json:"full_name"`
	SSHURL   string `json:"ssh_url"`
	HTMLURL  string `json:"html_url"`
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
	return ProjectInfo{Exists: true, Project: value.FullName, SSHURL: value.SSHURL, WebURL: value.HTMLURL}, nil
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
	return ProjectInfo{Exists: true, Project: value.FullName, SSHURL: value.SSHURL, WebURL: value.HTMLURL}, nil
}

type githubReviewResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Draft   bool   `json:"draft"`
	Head    struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (g *GitHub) FindOpenReviews(ctx context.Context, spec ReviewSpec) ([]ReviewInfo, error) {
	parts, err := splitProject("github", spec.Project, 2)
	if err != nil {
		return nil, err
	}
	endpoint := "repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/pulls?state=open&head=" + url.QueryEscape(parts[0]+":"+spec.SourceBranch) + "&base=" + url.QueryEscape(spec.TargetBranch)
	response, err := g.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var values []githubReviewResponse
	if err := decodeProviderResponse("github", response, &values); err != nil {
		return nil, err
	}
	return githubReviews(values, spec), nil
}

func (g *GitHub) CreateReview(ctx context.Context, spec ReviewSpec) (ReviewInfo, error) {
	parts, err := splitProject("github", spec.Project, 2)
	if err != nil {
		return ReviewInfo{}, err
	}
	response, err := g.request(ctx, http.MethodPost, "repos/"+url.PathEscape(parts[0])+"/"+url.PathEscape(parts[1])+"/pulls", map[string]any{
		"title": spec.Title, "head": spec.SourceBranch, "base": spec.TargetBranch, "body": spec.Description, "draft": spec.Draft,
	})
	if err != nil {
		return ReviewInfo{}, err
	}
	var value githubReviewResponse
	if err := decodeProviderResponse("github", response, &value); err != nil {
		return ReviewInfo{}, err
	}
	return githubReview(value, spec.Project), nil
}

func (g *GitHub) SetReady(ctx context.Context, review ReviewInfo) (ReviewInfo, error) {
	parts, err := splitProject("github", review.Project, 2)
	if err != nil {
		return ReviewInfo{}, err
	}
	response, err := g.request(ctx, http.MethodPatch, "repos/"+url.PathEscape(parts[0])+"/"+url.PathEscape(parts[1])+"/pulls/"+url.PathEscape(review.ID), map[string]any{"draft": false})
	if err != nil {
		return ReviewInfo{}, err
	}
	var value githubReviewResponse
	if err := decodeProviderResponse("github", response, &value); err != nil {
		return ReviewInfo{}, err
	}
	return githubReview(value, review.Project), nil
}

func githubReviews(values []githubReviewResponse, spec ReviewSpec) []ReviewInfo {
	result := make([]ReviewInfo, 0, len(values))
	for _, value := range values {
		if value.Head.Ref == spec.SourceBranch && value.Base.Ref == spec.TargetBranch {
			result = append(result, githubReview(value, spec.Project))
		}
	}
	return result
}

func githubReview(value githubReviewResponse, project ...string) ReviewInfo {
	result := ReviewInfo{ID: strconv.Itoa(value.Number), URL: value.HTMLURL, Title: value.Title, Description: value.Body, SourceBranch: value.Head.Ref, TargetBranch: value.Base.Ref, Draft: value.Draft}
	if len(project) > 0 {
		result.Project = project[0]
	}
	return result
}
