// Package provider contains small provider-neutral HTTP boundaries for
// GitHub and GitLab project/review automation.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ProjectSpec describes a fully qualified provider project to create.
type ProjectSpec struct {
	Project    string
	Visibility string
}

// ProjectInfo contains only safe identity and clone/review URLs.
type ProjectInfo struct {
	Exists  bool
	Project string
	SSHURL  string
	WebURL  string
}

// ReviewSpec identifies one source/target review and its safe content.
type ReviewSpec struct {
	Project      string
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
	Draft        bool
}

// ReviewInfo is the provider-neutral review result.
type ReviewInfo struct {
	Project      string
	ID           string
	URL          string
	Title        string
	Description  string
	SourceBranch string
	TargetBranch string
	Draft        bool
}

// ProjectProvider discovers and creates provider projects.
type ProjectProvider interface {
	InspectProject(context.Context, string) (ProjectInfo, error)
	CreateProject(context.Context, ProjectSpec) (ProjectInfo, error)
}

// ReviewProvider discovers, creates, and promotes open reviews.
type ReviewProvider interface {
	FindOpenReviews(context.Context, ReviewSpec) ([]ReviewInfo, error)
	CreateReview(context.Context, ReviewSpec) (ReviewInfo, error)
	SetReady(context.Context, ReviewInfo) (ReviewInfo, error)
}

type client struct {
	provider string
	baseURL  *url.URL
	token    string
	http     *http.Client
}

func (c client) endpoint() string { return c.baseURL.String() }

func newClient(provider, baseURL, token string, httpClient *http.Client) (client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return client{}, fmt.Errorf("%s provider API base URL is required", provider)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return client{}, fmt.Errorf("%s provider API base URL is invalid", provider)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/"
	return client{provider: provider, baseURL: parsed, token: token, http: httpClient}, nil
}

func (c client) request(ctx context.Context, method, endpoint string, body any) (*http.Response, error) {
	if strings.TrimSpace(c.token) == "" {
		return nil, fmt.Errorf("%s provider token is not configured", c.provider)
	}
	requestURL := *c.baseURL
	relative, err := url.Parse(strings.TrimLeft(endpoint, "/"))
	if err != nil {
		return nil, fmt.Errorf("%s provider request path is invalid", c.provider)
	}
	requestURL = *c.baseURL.ResolveReference(relative)
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("%s provider request body is invalid", c.provider)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("%s provider request could not be created", c.provider)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.provider == "github" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	} else {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}
	return c.http.Do(req)
}

func providerPathError(provider string, response *http.Response) error {
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return fmt.Errorf("%s provider request failed with status %d", provider, response.StatusCode)
}

func decodeProviderResponse(provider string, response *http.Response, target any) error {
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("%s provider request failed with status %d", provider, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("%s provider returned a malformed response", provider)
	}
	return nil
}

func inspectNotFound(provider string, response *http.Response) (bool, error) {
	if response.StatusCode == http.StatusNotFound {
		response.Body.Close()
		return true, nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return false, nil
	}
	return false, providerPathError(provider, response)
}

func splitProject(provider, project string, exactParts int) ([]string, error) {
	parts := strings.Split(project, "/")
	minimumParts := exactParts
	if provider == "gitlab" && exactParts == 0 {
		minimumParts = 2
	}
	if len(parts) < minimumParts || (exactParts > 0 && len(parts) != exactParts) {
		return nil, fmt.Errorf("%s project identity is invalid", provider)
	}
	for _, part := range parts {
		if !projectComponentPattern.MatchString(part) {
			return nil, fmt.Errorf("%s project identity is invalid", provider)
		}
	}
	return parts, nil
}

var errProviderResponse = errors.New("provider response is invalid")

var projectComponentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
