package git

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestHTTPSTokensRemainContainedForBothProviders(t *testing.T) {
	for _, test := range []struct{ provider, url, variable, token string }{{"github", "https://github.com/example/repo.git", "SMT_GITHUB_TOKEN", "github-secret"}, {"gitlab", "https://gitlab.com/example/repo.git", "SMT_GITLAB_TOKEN", "gitlab-secret"}} {
		t.Run(test.provider, func(t *testing.T) {
			t.Setenv(test.variable, test.token)
			auth, err := authFor(PushStep{RemoteURL: test.url, Provider: test.provider})
			if err != nil {
				t.Fatal(err)
			}
			if auth == nil {
				t.Fatal("auth is nil")
			}
			if strings.Contains(auth.String(), test.token) {
				t.Fatalf("auth string exposed token: %s", auth.String())
			}
			original := pushStep
			defer func() { pushStep = original }()
			pushStep = func(_ context.Context, _ PushStep) error { return fmt.Errorf("remote rejected %s", test.token) }
			report, pushErr := ExecutePush(context.Background(), PushPlan{Steps: []PushStep{{Repository: Repository{ID: "repo"}, Branch: "main"}}}, false)
			if pushErr == nil {
				t.Fatal("expected push failure")
			}
			if strings.Contains(pushErr.Error(), test.token) || strings.Contains(fmt.Sprintf("%#v", report), test.token) {
				t.Fatalf("public push result exposed token: error=%v report=%#v", pushErr, report)
			}
		})
	}
}
