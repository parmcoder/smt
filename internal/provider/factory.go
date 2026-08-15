package provider

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/parmcoder/smt/internal/config"
)

// NewConfigured constructs the provider boundary selected by configuration.
// Tokens are supplied by the caller so they can remain environment-only.
func NewConfigured(name string, settings config.ProviderConfig, token string, httpClient *http.Client) (ProjectProvider, error) {
	baseURL := strings.TrimSpace(settings.APIBaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(settings.EnterpriseBaseURL)
	}
	if baseURL == "" {
		switch name {
		case "github":
			baseURL = "https://api.github.com/"
		case "gitlab":
			baseURL = "https://gitlab.com/api/v4/"
		default:
			return nil, fmt.Errorf("unsupported provider %q", name)
		}
	}
	switch name {
	case "github":
		client, err := NewGitHub(baseURL, token, httpClient)
		if err != nil {
			return nil, err
		}
		return client, nil
	case "gitlab":
		client, err := NewGitLab(baseURL, token, httpClient)
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}
