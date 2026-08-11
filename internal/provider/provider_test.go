package provider

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	args         []string
	stdout       string
	stderr       string
	err          error
	block        bool
	deadline     time.Duration
	stdoutBuffer *limitedBuffer
	stderrBuffer *limitedBuffer
}

type sequenceClock struct {
	values []time.Time
}

func (c *sequenceClock) Now() time.Time {
	value := c.values[0]
	c.values = c.values[1:]
	return value
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	f.args = append([]string{name}, args...)
	if deadline, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(deadline)
	}
	f.stdoutBuffer, _ = stdout.(*limitedBuffer)
	f.stderrBuffer, _ = stderr.(*limitedBuffer)
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	_, _ = io.WriteString(stdout, f.stdout)
	_, _ = io.WriteString(stderr, f.stderr)
	return f.err
}

func TestBitwardenDefaultBudgetCoversGuardedCommandsBeyondTenSeconds(t *testing.T) {
	runner := &fakeRunner{stdout: "123456\n"}
	code, err := (Bitwarden{Runner: runner}).Code(context.Background(), "Example Login")
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(code)
	if runner.deadline < 19*time.Second || runner.deadline > 21*time.Second {
		t.Fatalf("provider deadline = %v, want approximately 20s", runner.deadline)
	}
}

func TestBitwardenReadyUsesStatusAndAcceptsUnlocked(t *testing.T) {
	runner := &fakeRunner{stdout: `{"status":"unlocked","serverUrl":"https://private.invalid"}`}
	err := callReady(t, Bitwarden{Runner: runner}, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bw", "status"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("args = %#v, want %#v", runner.args, want)
	}
}

func TestBitwardenReadyClearsCapturedStatusOutput(t *testing.T) {
	runner := &fakeRunner{
		stdout: `{"status":"unlocked","serverUrl":"https://private.invalid"}`,
		stderr: "private diagnostic",
	}
	if err := callReady(t, Bitwarden{Runner: runner}, context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, buffer := range map[string]*limitedBuffer{"stdout": runner.stdoutBuffer, "stderr": runner.stderrBuffer} {
		if buffer == nil {
			t.Fatalf("%s buffer was not captured", name)
		}
		for _, value := range buffer.Bytes() {
			if value != 0 {
				t.Fatalf("%s buffer retained provider output", name)
			}
		}
	}
}

func TestBitwardenReadyClassifiesStatusAndCommandFailuresSafely(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name    string
		runner  *fakeRunner
		ctx     context.Context
		kind    Kind
		message string
	}{
		{
			name:    "locked status",
			runner:  &fakeRunner{stdout: `{"status":"locked","serverUrl":"https://private.invalid"}`},
			ctx:     context.Background(),
			kind:    Locked,
			message: "Bitwarden vault is locked or unavailable after <1s",
		},
		{
			name:    "unauthenticated status",
			runner:  &fakeRunner{stdout: `{"status":"unauthenticated"}`},
			ctx:     context.Background(),
			kind:    Locked,
			message: "Bitwarden vault is locked or unavailable after <1s",
		},
		{
			name:    "malformed status",
			runner:  &fakeRunner{stdout: `{"status":`, stderr: "private diagnostic"},
			ctx:     context.Background(),
			kind:    Failed,
			message: "Bitwarden readiness check failed after <1s",
		},
		{
			name:    "missing executable",
			runner:  &fakeRunner{err: &exec.Error{Name: "bw", Err: exec.ErrNotFound}},
			ctx:     context.Background(),
			kind:    Unavailable,
			message: "Bitwarden CLI is unavailable after <1s",
		},
		{
			name:    "timeout",
			runner:  &fakeRunner{block: true},
			ctx:     context.Background(),
			kind:    TimedOut,
			message: "Bitwarden readiness check timed out after <1s",
		},
		{
			name:    "interrupted",
			runner:  &fakeRunner{block: true},
			ctx:     canceled,
			kind:    Interrupted,
			message: "Bitwarden readiness check was interrupted after <1s",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := Bitwarden{Runner: test.runner}
			if test.name == "timeout" {
				source.Timeout = time.Millisecond
			}
			err := callReady(t, source, test.ctx)
			if KindOf(err) != test.kind {
				t.Fatalf("kind = %q, err = %v", KindOf(err), err)
			}
			if got := SafeMessage(err); got != test.message {
				t.Fatalf("safe message = %q, want %q", got, test.message)
			}
			for _, forbidden := range []string{"private.invalid", "private diagnostic", "TOTP"} {
				if strings.Contains(SafeMessage(err), forbidden) {
					t.Fatalf("safe message leaked %q: %q", forbidden, SafeMessage(err))
				}
			}
		})
	}
}

func TestBitwardenReadyRejectsOversizedStatusOutput(t *testing.T) {
	runner := &fakeRunner{stdout: strings.Repeat("x", maxStatusOutput+1)}
	err := callReady(t, Bitwarden{Runner: runner}, context.Background())
	if KindOf(err) != Failed || SafeMessage(err) != "Bitwarden readiness check failed after <1s" {
		t.Fatalf("kind = %q, safe message = %q", KindOf(err), SafeMessage(err))
	}
	for _, value := range runner.stdoutBuffer.Bytes() {
		if value != 0 {
			t.Fatal("oversized status output was not cleared")
		}
	}
}

func TestSafeReadinessMessageRedactsUnknownErrors(t *testing.T) {
	message := SafeReadinessMessage(errors.New("private provider detail"))
	if message != "Bitwarden readiness check failed" {
		t.Fatalf("safe readiness message = %q", message)
	}
}

func TestBitwardenFailureDiagnosticsIncludeBoundedElapsedTime(t *testing.T) {
	base := time.Unix(0, 0)
	tests := []struct {
		name     string
		ready    bool
		duration time.Duration
		want     string
		seconds  int
	}{
		{name: "subsecond retrieval", duration: 200 * time.Millisecond, want: "Bitwarden TOTP retrieval failed after <1s", seconds: 0},
		{name: "nearest second readiness", ready: true, duration: 4600 * time.Millisecond, want: "Bitwarden vault is locked or unavailable after 5s", seconds: 5},
		{name: "deadline clamp", duration: 25 * time.Second, want: "Bitwarden TOTP retrieval failed after 20s", seconds: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &sequenceClock{values: []time.Time{base, base.Add(test.duration)}}
			runner := &fakeRunner{err: errors.New("private failure")}
			source := Bitwarden{Runner: runner, Now: clock.Now}
			var err error
			if test.ready {
				runner.err = nil
				runner.stdout = `{"status":"locked","serverUrl":"https://private.invalid"}`
				err = source.Ready(context.Background())
			} else {
				_, err = source.Code(context.Background(), "Private Item")
			}
			message := SafeMessage(err)
			if test.ready {
				message = SafeReadinessMessage(err)
			}
			if message != test.want {
				t.Fatalf("safe message = %q, want %q", message, test.want)
			}
			seconds, ok := ElapsedSeconds(err)
			if !ok || seconds != test.seconds {
				t.Fatalf("elapsed = %d, %v; want %d, true", seconds, ok, test.seconds)
			}
			for _, private := range []string{"Private Item", "private failure", "private.invalid"} {
				if strings.Contains(message, private) {
					t.Fatalf("message leaked %q: %q", private, message)
				}
			}
		})
	}
}

func TestUnmeasuredErrorsDoNotInventElapsedTime(t *testing.T) {
	err := errors.New("private failure")
	if got := SafeMessage(err); got != "Bitwarden TOTP retrieval failed" {
		t.Fatalf("safe message = %q", got)
	}
	if seconds, ok := ElapsedSeconds(err); ok || seconds != 0 {
		t.Fatalf("elapsed = %d, %v; want 0, false", seconds, ok)
	}
}

func callReady(t *testing.T, source Bitwarden, ctx context.Context) error {
	t.Helper()
	ready, ok := any(source).(interface {
		Ready(context.Context) error
	})
	if !ok {
		t.Fatal("Bitwarden does not implement readiness")
	}
	return ready.Ready(ctx)
}

func TestBitwardenUsesExactArguments(t *testing.T) {
	runner := &fakeRunner{stdout: "123456\n"}
	code, err := (Bitwarden{Runner: runner}).Code(context.Background(), "Example Login")
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(code)
	if string(code) != "123456" {
		t.Fatalf("code = %q", code)
	}
	want := []string{"bw", "get", "totp", "Example Login"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("args = %#v, want %#v", runner.args, want)
	}
}

func TestProviderClassification(t *testing.T) {
	tests := []struct {
		message string
		kind    Kind
	}{
		{"Vault is locked.", Locked},
		{"More than one result was found.", Ambiguous},
		{"Item not found.", Missing},
		{"Unexpected failure.", Failed},
	}
	for _, test := range tests {
		runner := &fakeRunner{stderr: test.message, err: errors.New("exit 1")}
		_, err := (Bitwarden{Runner: runner}).Code(context.Background(), "Synthetic")
		if KindOf(err) != test.kind {
			t.Fatalf("%q: got %q", test.message, KindOf(err))
		}
		if strings.Contains(SafeMessage(err), "Synthetic") {
			t.Fatal("safe message contains item")
		}
	}
}

func TestProviderTimeout(t *testing.T) {
	runner := &fakeRunner{block: true}
	_, err := (Bitwarden{Runner: runner, Timeout: time.Millisecond}).Code(context.Background(), "Synthetic")
	if KindOf(err) != TimedOut {
		t.Fatalf("kind = %q, err = %v", KindOf(err), err)
	}
}

func TestProviderRejectsOversizedOutput(t *testing.T) {
	runner := &fakeRunner{stdout: strings.Repeat("1", maxCodeOutput+1)}
	_, err := (Bitwarden{Runner: runner}).Code(context.Background(), "Synthetic")
	if KindOf(err) != Invalid {
		t.Fatalf("kind = %q, err = %v", KindOf(err), err)
	}
}
