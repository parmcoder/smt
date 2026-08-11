package provider

import (
	"net/http"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestNewConfiguredUsesCustomAndDefaultEndpoints(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider string
		settings config.ProviderConfig
		want     string
	}{
		{name: "github custom", provider: "github", settings: config.ProviderConfig{APIBaseURL: "https://github.example/api/v3/"}, want: "https://github.example/api/v3/"},
		{name: "gitlab enterprise", provider: "gitlab", settings: config.ProviderConfig{EnterpriseBaseURL: "https://gitlab.example/api/v4"}, want: "https://gitlab.example/api/v4/"},
		{name: "github default", provider: "github", want: "https://api.github.com/"},
		{name: "gitlab default", provider: "gitlab", want: "https://gitlab.com/api/v4/"},
	} {
		t.Run(test.name, func(t *testing.T) {
			projects, _, err := NewConfigured(test.provider, test.settings, "token", &http.Client{})
			if err != nil {
				t.Fatal(err)
			}
			got := projects.(interface{ endpoint() string }).endpoint()
			if got != test.want {
				t.Fatalf("endpoint=%q want %q", got, test.want)
			}
		})
	}
}
