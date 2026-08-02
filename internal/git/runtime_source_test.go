package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitRuntimeSourcesDoNotSpawnSystemGit(t *testing.T) {
	for _, name := range []string{"repository.go", "push.go"} {
		contents, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), "os/exec") || strings.Contains(string(contents), "ExecRunner") {
			t.Fatalf("%s retains a system-Git runner", name)
		}
	}
}
