package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const (
	DefaultTimeout = 10 * time.Second
	maxCodeOutput  = 128
	maxErrorOutput = 1024
)

type Kind string

const (
	Unavailable Kind = "unavailable"
	Locked      Kind = "locked"
	Missing     Kind = "missing"
	Ambiguous   Kind = "ambiguous"
	Invalid     Kind = "invalid"
	TimedOut    Kind = "timeout"
	Interrupted Kind = "interrupted"
	Failed      Kind = "failed"
)

type Error struct {
	Kind Kind
}

func (e *Error) Error() string {
	switch e.Kind {
	case Unavailable:
		return "Bitwarden CLI is unavailable"
	case Locked:
		return "Bitwarden vault is locked or unavailable"
	case Missing:
		return "Bitwarden item was not found"
	case Ambiguous:
		return "Bitwarden item reference is ambiguous"
	case Invalid:
		return "Bitwarden returned an invalid TOTP value"
	case TimedOut:
		return "Bitwarden TOTP retrieval timed out"
	case Interrupted:
		return "Bitwarden TOTP retrieval was interrupted"
	default:
		return "Bitwarden TOTP retrieval failed"
	}
}

type Source interface {
	Code(context.Context, string) ([]byte, error)
}

type AwaitingSource interface {
	Source
	AwaitCode(context.Context, string) ([]byte, error)
}

type Runner interface {
	Run(context.Context, string, []string, io.Writer, io.Writer) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

type Bitwarden struct {
	Runner  Runner
	Timeout time.Duration
}

func (b Bitwarden) Code(ctx context.Context, item string) ([]byte, error) {
	runner := b.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout := &limitedBuffer{limit: maxCodeOutput}
	stderr := &limitedBuffer{limit: maxErrorOutput}
	err := runner.Run(runCtx, "bw", []string{"get", "totp", item}, stdout, stderr)
	if err != nil {
		return nil, classify(runCtx, err, stderr.String())
	}
	code := bytes.TrimSpace(stdout.Bytes())
	if len(code) == 0 || stdout.exceeded {
		return nil, &Error{Kind: Invalid}
	}
	result := append([]byte(nil), code...)
	zeroBytes(code)
	return result, nil
}

func classify(ctx context.Context, err error, message string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Kind: TimedOut}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return &Error{Kind: Interrupted}
	}
	var execError *exec.Error
	if errors.As(err, &execError) {
		return &Error{Kind: Unavailable}
	}
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "vault is locked"), strings.Contains(lower, "not logged in"), strings.Contains(lower, "session key is invalid"):
		return &Error{Kind: Locked}
	case strings.Contains(lower, "multiple results"), strings.Contains(lower, "more than one"):
		return &Error{Kind: Ambiguous}
	case strings.Contains(lower, "not found"), strings.Contains(lower, "no item"):
		return &Error{Kind: Missing}
	default:
		return &Error{Kind: Failed}
	}
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (w *limitedBuffer) Write(value []byte) (int, error) {
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.exceeded = true
		return len(value), nil
	}
	if len(value) > remaining {
		w.exceeded = true
		_, _ = w.buffer.Write(value[:remaining])
		return len(value), nil
	}
	_, _ = w.buffer.Write(value)
	return len(value), nil
}

func (w *limitedBuffer) Bytes() []byte {
	return w.buffer.Bytes()
}

func (w *limitedBuffer) String() string {
	return w.buffer.String()
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func KindOf(err error) Kind {
	var providerError *Error
	if errors.As(err, &providerError) {
		return providerError.Kind
	}
	return Failed
}

func SafeMessage(err error) string {
	var providerError *Error
	if errors.As(err, &providerError) {
		return providerError.Error()
	}
	return fmt.Sprintf("%s", (&Error{Kind: Failed}).Error())
}
