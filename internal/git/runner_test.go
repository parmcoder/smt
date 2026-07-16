package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecRunnerCapturesOutput(t *testing.T) {
	result, err := (ExecRunner{}).Run(context.Background(), ".", "version")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "git version") {
		t.Fatalf("Stdout = %q, want Git version", result.Stdout)
	}
}

func TestExecRunnerReturnsExitCodeAndRepositoryError(t *testing.T) {
	result, err := (ExecRunner{}).Run(context.Background(), t.TempDir()+"/missing", "status")
	if err == nil {
		t.Fatal("Run() error = nil, want command failure")
	}
	if result.ExitCode == 0 {
		t.Fatalf("ExitCode = %d, want non-zero", result.ExitCode)
	}
	if !strings.Contains(err.Error(), "status") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v, want operation and repository directory", err)
	}
}

func TestExecRunnerCapturesStandardErrorOnSuccess(t *testing.T) {
	dir := t.TempDir()
	gitPath := filepath.Join(dir, "git")
	if err := os.WriteFile(gitPath, []byte("#!/bin/sh\nprintf stdout\nprintf stderr >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	result, err := (ExecRunner{}).Run(context.Background(), ".", "version")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Stdout != "stdout" || result.Stderr != "stderr" {
		t.Fatalf("Result = %#v, want both standard streams", result)
	}
}
