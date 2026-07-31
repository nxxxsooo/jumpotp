package workspace

import (
	"context"
	"io"
	"os/exec"
	"time"
)

type Runner interface {
	Output(context.Context, string, ...string) ([]byte, error)
	Interactive(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error
	LookPath(string) (string, error)
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.WaitDelay = 500 * time.Millisecond
	return command.CombinedOutput()
}

func (ExecRunner) Interactive(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func (ExecRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}
