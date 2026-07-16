package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseAutomationContract(t *testing.T) {
	root := repositoryRoot(t)
	taskfile := readRepositoryFile(t, root, "Taskfile.yml")
	gitignore := readRepositoryFile(t, root, ".gitignore")
	workflow := readRepositoryFile(t, root, filepath.Join(".github", "workflows", "release.yml"))
	if !strings.Contains(gitignore, "/dist/") {
		t.Error(".gitignore missing /dist/ generated-artifact rule")
	}

	for _, want := range []string{
		"release:build:",
		"release:tag:",
		"^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$",
		"CGO_ENABLED=0",
		"go build -trimpath",
		"dist/smt_{{.VERSION}}_",
		"tar -czf",
		"-C \"$tmp\" smt",
		"shasum -a 256",
		"git status --porcelain --untracked-files=all",
		"git tag -a \"{{.VERSION}}\"",
		"git push origin \"refs/tags/{{.VERSION}}\"",
		"task verify",
	} {
		if !strings.Contains(taskfile, want) {
			t.Errorf("Taskfile.yml missing %q", want)
		}
	}

	for _, want := range []string{
		"push:",
		"tags: [v*.*.*]",
		"contents: read",
		"contents: write",
		"checkout@v5",
		"setup-go@v5",
		"upload-artifact@v4",
		"download-artifact@v4",
		"os: linux\n            arch: amd64",
		"os: linux\n            arch: arm64",
		"os: darwin\n            arch: amd64",
		"os: darwin\n            arch: arm64",
		"github.ref_name",
		"CGO_ENABLED: 0",
		"-trimpath",
		"dist/smt_${VERSION}_${OS}_${ARCH}.tar.gz",
		"sha256sum",
		"GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
		"gh release create \"$GITHUB_REF_NAME\" --verify-tag --generate-notes",
		"smt_${VERSION}_linux_amd64.tar.gz",
		"smt_${VERSION}_linux_arm64.tar.gz",
		"smt_${VERSION}_darwin_amd64.tar.gz",
		"smt_${VERSION}_darwin_arm64.tar.gz",
		"checksums.txt",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	root, err := filepath.Abs(filepath.Join(workingDirectory, "..", ".."))
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	return root
}

func readRepositoryFile(t *testing.T, root, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", name, err)
	}
	return string(content)
}
