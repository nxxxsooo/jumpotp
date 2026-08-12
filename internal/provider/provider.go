package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

const (
	DefaultTimeout  = 20 * time.Second
	maxCodeOutput   = 128
	maxStatusOutput = 4096
	maxErrorOutput  = 1024
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
	Kind           Kind
	operation      operation
	elapsedSeconds int
	measured       bool
}

type operation string

const readiness operation = "readiness"

func (e *Error) Error() string {
	var message string
	if e.operation == readiness {
		switch e.Kind {
		case Unavailable:
			message = "Bitwarden CLI is unavailable"
		case Locked:
			message = "Bitwarden vault is locked or unavailable"
		case TimedOut:
			message = "Bitwarden readiness check timed out"
		case Interrupted:
			message = "Bitwarden readiness check was interrupted"
		default:
			message = "Bitwarden readiness check failed"
		}
	} else {
		switch e.Kind {
		case Unavailable:
			message = "Bitwarden CLI is unavailable"
		case Locked:
			message = "Bitwarden vault is locked or unavailable"
		case Missing:
			message = "Bitwarden item was not found"
		case Ambiguous:
			message = "Bitwarden item reference is ambiguous"
		case Invalid:
			message = "Bitwarden returned an invalid TOTP value"
		case TimedOut:
			message = "Bitwarden TOTP retrieval timed out"
		case Interrupted:
			message = "Bitwarden TOTP retrieval was interrupted"
		default:
			message = "Bitwarden TOTP retrieval failed"
		}
	}
	if e.measured {
		if e.elapsedSeconds == 0 {
			return message + " after <1s"
		}
		return fmt.Sprintf("%s after %ds", message, e.elapsedSeconds)
	}
	return message
}

type Source interface {
	Code(context.Context, string) ([]byte, error)
}

type ReadinessSource interface {
	Ready(context.Context) error
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
	Now     func() time.Time
}

func (b Bitwarden) Ready(ctx context.Context) error {
	runner := b.runner()
	runCtx, cancel := b.runContext(ctx)
	defer cancel()

	stdout := &limitedBuffer{limit: maxStatusOutput}
	stderr := &limitedBuffer{limit: maxErrorOutput}
	defer func() { zeroBytes(stdout.Bytes()) }()
	defer func() { zeroBytes(stderr.Bytes()) }()
	started := b.now()
	err := runner.Run(runCtx, "bw", []string{"status"}, stdout, stderr)
	elapsed := b.elapsedSeconds(b.now().Sub(started))
	if err != nil {
		return classifyReadiness(runCtx, err, stderr.String(), elapsed)
	}
	if stdout.exceeded {
		return measuredFailure(Failed, readiness, elapsed)
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		return measuredFailure(Failed, readiness, elapsed)
	}
	if status.Status != "unlocked" {
		return measuredFailure(Locked, readiness, elapsed)
	}
	return nil
}

func (b Bitwarden) Code(ctx context.Context, item string) ([]byte, error) {
	runner := b.runner()
	runCtx, cancel := b.runContext(ctx)
	defer cancel()

	stdout := &limitedBuffer{limit: maxCodeOutput}
	stderr := &limitedBuffer{limit: maxErrorOutput}
	started := b.now()
	err := runner.Run(runCtx, "bw", []string{"get", "totp", item}, stdout, stderr)
	elapsed := b.elapsedSeconds(b.now().Sub(started))
	if err != nil {
		classified := classify(runCtx, err, stderr.String())
		return nil, measuredFailure(KindOf(classified), "", elapsed)
	}
	code := bytes.TrimSpace(stdout.Bytes())
	if len(code) == 0 || stdout.exceeded {
		return nil, measuredFailure(Invalid, "", elapsed)
	}
	result := append([]byte(nil), code...)
	zeroBytes(code)
	return result, nil
}

func (b Bitwarden) runner() Runner {
	if b.Runner != nil {
		return b.Runner
	}
	return ExecRunner{}
}

func (b Bitwarden) runContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (b Bitwarden) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b Bitwarden) elapsedSeconds(elapsed time.Duration) int {
	if elapsed < time.Second {
		return 0
	}
	seconds := int((elapsed + time.Second/2) / time.Second)
	maximum := int(DefaultTimeout / time.Second)
	if seconds > maximum {
		return maximum
	}
	return seconds
}

func classifyReadiness(ctx context.Context, err error, message string, elapsed int) error {
	kind := KindOf(classify(ctx, err, message))
	switch kind {
	case Unavailable, Locked, TimedOut, Interrupted:
		return measuredFailure(kind, readiness, elapsed)
	default:
		return measuredFailure(Failed, readiness, elapsed)
	}
}

func readinessFailure(kind Kind) error {
	return &Error{Kind: kind, operation: readiness}
}

func measuredFailure(kind Kind, operation operation, elapsedSeconds int) error {
	return &Error{Kind: kind, operation: operation, elapsedSeconds: elapsedSeconds, measured: true}
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

func SafeReadinessMessage(err error) string {
	seconds, measured := ElapsedSeconds(err)
	return SafeMessage(&Error{Kind: KindOf(err), operation: readiness, elapsedSeconds: seconds, measured: measured})
}

func ElapsedSeconds(err error) (int, bool) {
	var providerError *Error
	if errors.As(err, &providerError) && providerError.measured {
		return providerError.elapsedSeconds, true
	}
	return 0, false
}

func NewMeasuredError(kind Kind, elapsedSeconds int) error {
	if elapsedSeconds < 0 || elapsedSeconds > int(DefaultTimeout/time.Second) {
		return &Error{Kind: kind}
	}
	return measuredFailure(kind, "", elapsedSeconds)
}
