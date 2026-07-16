// Package git executes safe Git commands and inspects repositories.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Result contains the captured result of a Git command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner executes Git subcommands in a repository directory.
type Runner interface {
	Run(context.Context, string, ...string) (Result, error)
}

// ExecRunner executes Git through the operating system process runner.
type ExecRunner struct{}

// Run invokes Git with the supplied repository directory and argument array.
func (ExecRunner) Run(ctx context.Context, dir string, args ...string) (Result, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	} else {
		result.ExitCode = -1
	}
	if err != nil {
		operation := ""
		if len(args) > 0 {
			operation = args[0]
		}
		return result, fmt.Errorf("git %s in %s: %w", operation, dir, err)
	}
	return result, nil
}
