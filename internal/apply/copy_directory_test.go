package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestCopyDirectoryPreservesInternalRelativeSymlink(t *testing.T) {
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "web-app")
	target := filepath.Join(source, "node_modules", ".pnpm", "example@1.0.0", "node_modules", "example")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"name":"example"}\n`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(source, "node_modules", "example")
	if err := os.Symlink(filepath.Join(".pnpm", "example@1.0.0", "node_modules", "example"), link); err != nil {
		t.Fatal(err)
	}

	if err := copyDirectory(source, destination); err != nil {
		t.Fatalf("copyDirectory() error = %v", err)
	}

	copiedLink := filepath.Join(destination, "node_modules", "example")
	info, err := os.Lstat(copiedLink)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("copied dependency mode = %v, want symlink", info.Mode())
	}
	got, err := os.Readlink(copiedLink)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(".pnpm", "example@1.0.0", "node_modules", "example")
	if got != want {
		t.Fatalf("copied symlink target = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(copiedLink, "package.json")); err != nil {
		t.Fatalf("copied symlink target is not usable: %v", err)
	}
}

func TestCopyDirectoryRejectsUnsafeSymlinksWithContext(t *testing.T) {
	tests := []struct {
		name   string
		target func(string) (string, error)
		want   string
	}{
		{
			name: "absolute",
			target: func(source string) (string, error) {
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
					return "", err
				}
				return outside, nil
			},
			want: "absolute symlink target",
		},
		{
			name: "dangling",
			target: func(string) (string, error) {
				return filepath.Join(".pnpm", "missing"), nil
			},
			want: "dangling symlink target",
		},
		{
			name: "outside",
			target: func(source string) (string, error) {
				outside := filepath.Join(filepath.Dir(source), "outside")
				if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
					return "", err
				}
				return filepath.Join("..", "..", "outside"), nil
			},
			want: "target escapes staged component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := t.TempDir()
			destination := filepath.Join(t.TempDir(), "web-app")
			linkDir := filepath.Join(source, "node_modules")
			if err := os.MkdirAll(linkDir, 0o755); err != nil {
				t.Fatal(err)
			}
			target, err := tt.target(source)
			if err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(linkDir, "example")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}

			err = copyDirectory(source, destination)
			if err == nil {
				t.Fatal("copyDirectory() error = nil, want unsafe symlink error")
			}
			message := err.Error()
			for _, want := range []string{
				"source:",
				"node_modules/example",
				"destination:",
				"web-app/node_modules/example",
				"symlink target:",
				target,
				tt.want,
			} {
				if !strings.Contains(message, want) {
					t.Errorf("copyDirectory() error = %q, missing %q", message, want)
				}
			}
		})
	}
}

func TestApplyRejectsUnsafeWebDependencySymlinkAtomically(t *testing.T) {
	installFakeNextASDF(t, false)
	original := runDependencySetup
	t.Cleanup(func() { runDependencySetup = original })
	runDependencySetup = func(_ context.Context, cwd string, args []string) ([]byte, error) {
		if got := strings.Join(args, " "); got != "exec pnpm install" {
			return nil, errors.New("unexpected setup command: " + got)
		}
		if err := os.WriteFile(filepath.Join(cwd, "pnpm-lock.yaml"), []byte("lock\n"), 0o644); err != nil {
			return nil, err
		}
		linkDir := filepath.Join(cwd, "node_modules")
		if err := os.MkdirAll(linkDir, 0o755); err != nil {
			return nil, err
		}
		outside := filepath.Join(filepath.Dir(cwd), "outside")
		if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
			return nil, err
		}
		if err := os.Symlink(filepath.Join("..", "..", "outside"), filepath.Join(linkDir, "escape")); err != nil {
			return nil, err
		}
		return nil, nil
	}

	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	raw := blueprintBytes()
	cfg, err := config.LoadBytes(raw, filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}

	err = service.Apply(context.Background(), destination, raw)
	if err == nil {
		t.Fatal("Apply() error = nil, want unsafe Web dependency symlink error")
	}
	message := err.Error()
	for _, want := range []string{
		"initialize staged workspace",
		"publish web child failed",
		"source: .smt/bootstrap/web/node_modules/escape",
		"destination: web-app/node_modules/escape",
		"symlink target: ../../outside",
		"target escapes staged component",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("Apply() error = %q, missing %q", message, want)
		}
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("published destination exists after unsafe symlink failure: %v", statErr)
	}
	assertNoStage(t, parent)
}

func TestApplyPublishesInternalWebDependencySymlink(t *testing.T) {
	installFakeNextASDF(t, false)
	original := runDependencySetup
	t.Cleanup(func() { runDependencySetup = original })
	runDependencySetup = func(_ context.Context, cwd string, args []string) ([]byte, error) {
		if got := strings.Join(args, " "); got != "exec pnpm install" {
			return nil, errors.New("unexpected setup command: " + got)
		}
		if err := os.WriteFile(filepath.Join(cwd, "pnpm-lock.yaml"), []byte("lock\n"), 0o644); err != nil {
			return nil, err
		}
		target := filepath.Join(cwd, "node_modules", ".pnpm", "example@1.0.0", "node_modules", "example")
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"name":"example"}\n`), 0o644); err != nil {
			return nil, err
		}
		if err := os.Symlink(filepath.Join(".pnpm", "example@1.0.0", "node_modules", "example"), filepath.Join(cwd, "node_modules", "example")); err != nil {
			return nil, err
		}
		return nil, nil
	}

	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	raw := blueprintBytes()
	cfg, err := config.LoadBytes(raw, filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, raw); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(destination, "web-app", "node_modules", "example")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("published dependency mode = %v, want symlink", info.Mode())
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(".pnpm", "example@1.0.0", "node_modules", "example")
	if got != want {
		t.Fatalf("published dependency symlink target = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(link, "package.json")); err != nil {
		t.Fatalf("published dependency symlink target is not usable: %v", err)
	}
}
