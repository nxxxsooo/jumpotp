package terminal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/nxxxsooo/jumpotp/internal/config"
	"github.com/nxxxsooo/jumpotp/internal/provider"
)

type fakeSource struct {
	mu    sync.Mutex
	code  string
	err   error
	calls int
	items []string
}

type reconnectingSource struct {
	codeReady chan struct{}
	canceled  chan struct{}
}

type delayedSource struct {
	started chan struct{}
	release chan struct{}
	code    string
	err     error
}

func (s *reconnectingSource) Code(context.Context, string) ([]byte, error) {
	return nil, &provider.Error{Kind: provider.Unavailable}
}

func (s *reconnectingSource) AwaitCode(ctx context.Context, _ string) ([]byte, error) {
	select {
	case <-s.codeReady:
		return []byte("135790"), nil
	case <-ctx.Done():
		close(s.canceled)
		return nil, ctx.Err()
	}
}

func (s *delayedSource) Code(ctx context.Context, _ string) ([]byte, error) {
	close(s.started)
	select {
	case <-s.release:
		if s.err != nil {
			return nil, s.err
		}
		return []byte(s.code), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeSource) Code(_ context.Context, item string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.items = append(f.items, item)
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.code), nil
}

func (f *fakeSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestAutomaticInjectionThroughRealPTY(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read code
if [ "$code" = "246810" ]; then
  printf '\nAUTH_OK\n'
  exit 0
fi
printf '\nAUTH_FAILED\n'
exit 9
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	source := &fakeSource{code: "246810"}
	var out, errOut bytes.Buffer
	result := Connect(context.Background(), Options{
		Target: syntheticTarget(),
		Source: source,
		In:     input,
		Out:    &out,
		Err:    &errOut,
	})
	if result.ExitCode != 0 || result.Err != nil {
		t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "AUTH_OK") {
		t.Fatalf("stdout = %q", out.String())
	}
	if source.callCount() != 1 {
		t.Fatalf("provider calls = %d", source.callCount())
	}
	if strings.Contains(errOut.String(), "246810") {
		t.Fatalf("diagnostics contain OTP: %q", errOut.String())
	}
	if strings.Contains(out.String(), " after ") || strings.Contains(errOut.String(), " after ") {
		t.Fatalf("successful retrieval emitted timing: stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

func TestPasswordPromptDoesNotCallProvider(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Password: '
IFS= read password
if [ "$password" = "synthetic-password" ]; then
  printf '\nPASSWORD_OK\n'
  exit 0
fi
exit 8
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("synthetic-password\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{code: "246810"}
	var out, errOut bytes.Buffer
	result := Connect(context.Background(), Options{
		Target: syntheticTarget(),
		Source: source,
		In:     input,
		Out:    &out,
		Err:    &errOut,
	})
	if result.ExitCode != 0 {
		t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
	}
	if source.callCount() != 0 {
		t.Fatalf("provider calls = %d", source.callCount())
	}
}

func TestRejectedCodeIsNotAutomaticallyRetried(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read first
printf '\nEnter 6-digit verification code: '
IFS= read second
exit 7
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	target := syntheticTarget()
	target.Fallback = "fail"
	source := &fakeSource{code: "246810"}
	var out, errOut bytes.Buffer
	result := Connect(context.Background(), Options{
		Target: target,
		Source: source,
		In:     input,
		Out:    &out,
		Err:    &errOut,
	})
	if result.ExitCode != 4 {
		t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
	}
	if source.callCount() != 1 {
		t.Fatalf("provider calls = %d", source.callCount())
	}
}

func TestInputWhileAutomaticCodeIsPendingDoesNotCorruptSubmission(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read code
if [ "$code" = "246810" ]; then
  printf '\nAUTH_OK\n'
  exit 0
fi
printf '\nAUTH_FAILED\n'
exit 9
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	localMaster, localTTY, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer localMaster.Close()
	defer localTTY.Close()
	source := &delayedSource{
		started: make(chan struct{}),
		release: make(chan struct{}),
		code:    "246810",
	}
	var out safeBuffer
	var errOut safeBuffer
	done := make(chan Result, 1)
	go func() {
		done <- Connect(context.Background(), Options{
			Target: syntheticTarget(),
			Source: source,
			In:     localTTY,
			Out:    &out,
			Err:    &errOut,
		})
	}()
	select {
	case <-source.started:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic provider did not start")
	}
	if _, err := localMaster.Write([]byte{0x02, 'd'}); err != nil {
		t.Fatal(err)
	}
	close(source.release)
	select {
	case result := <-done:
		if result.ExitCode != 0 || result.Err != nil {
			t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "AUTH_OK") {
			t.Fatalf("stdout = %q", out.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("automatic submission timed out")
	}
}

func TestPendingInputKeepsInterruptControls(t *testing.T) {
	value := []byte{0x02, 'd', 0x03, 0x1a, 0x1c}
	var forwarded bytes.Buffer
	if err := forwardPendingControls(&forwarded, value); err != nil {
		t.Fatal(err)
	}
	if got, want := forwarded.Bytes(), []byte{0x03, 0x1a, 0x1c}; !bytes.Equal(got, want) {
		t.Fatalf("forwarded controls = %v, want %v", got, want)
	}
	if !bytes.Equal(value, make([]byte, len(value))) {
		t.Fatalf("pending input was not cleared: %v", value)
	}
}

func TestProviderFailureWithoutTTYFailsClosed(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read code
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	source := &fakeSource{err: errors.New("synthetic provider failure")}
	var out, errOut bytes.Buffer
	result := Connect(context.Background(), Options{
		Target: syntheticTarget(),
		Source: source,
		In:     input,
		Out:    &out,
		Err:    &errOut,
	})
	if result.ExitCode != 4 || result.Err == nil {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(result.Err.Error(), "synthetic provider failure") {
		t.Fatalf("unclassified provider detail leaked: %v", result.Err)
	}
}

func TestVisibleManualFallbackOnTTY(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read code
if [ "$code" = "135790" ]; then
  printf '\nMANUAL_OK\n'
  exit 0
fi
exit 6
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	localMaster, localTTY, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer localMaster.Close()
	defer localTTY.Close()
	source := &fakeSource{err: provider.NewMeasuredError(provider.TimedOut, 6)}
	var out safeBuffer
	var errOut safeBuffer
	done := make(chan Result, 1)
	go func() {
		done <- Connect(context.Background(), Options{
			Target: syntheticTarget(),
			Source: source,
			In:     localTTY,
			Out:    &out,
			Err:    &errOut,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "digits are visible") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "digits are visible") {
		t.Fatalf("manual prompt not shown: stdout = %q, stderr = %q", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Bitwarden TOTP retrieval timed out after 6s") {
		t.Fatalf("timed fallback reason missing: stdout = %q", out.String())
	}
	if _, err := localMaster.Write([]byte("135790\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.ExitCode != 0 {
			t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "MANUAL_OK") {
			t.Fatalf("stdout = %q", out.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("manual fallback timed out")
	}
}

func TestVisibleManualFallbackDoesNotTreatLeadingControlDAsCode(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read code
if [ "$code" = "135790" ]; then
  printf '\nMANUAL_OK\n'
  exit 0
fi
exit 6
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	localMaster, localTTY, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer localMaster.Close()
	defer localTTY.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out safeBuffer
	var errOut safeBuffer
	done := make(chan Result, 1)
	go func() {
		done <- Connect(ctx, Options{
			Target: syntheticTarget(),
			Source: &fakeSource{err: errors.New("synthetic provider failure")},
			In:     localTTY,
			Out:    &out,
			Err:    &errOut,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "digits are visible") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "digits are visible") {
		t.Fatalf("manual prompt not shown: stdout = %q, stderr = %q", out.String(), errOut.String())
	}
	if _, err := localMaster.Write([]byte{0x04}); err != nil {
		t.Fatal(err)
	}
	if _, err := localMaster.Write([]byte("135790\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.ExitCode != 0 || result.Err != nil {
			t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "MANUAL_OK") {
			t.Fatalf("stdout = %q", out.String())
		}
		if strings.Contains(out.String(), "code must contain only digits") {
			t.Fatalf("control-D contaminated manual code: %q", out.String())
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatalf("manual fallback timed out: stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

func TestVisibleManualFallbackProcessesBufferedRetry(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read code
if [ "$code" = "135790" ]; then
  printf '\nMANUAL_OK\n'
  exit 0
fi
exit 6
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	localMaster, localTTY, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer localMaster.Close()
	defer localTTY.Close()
	var out safeBuffer
	var errOut safeBuffer
	done := make(chan Result, 1)
	go func() {
		done <- Connect(context.Background(), Options{
			Target: syntheticTarget(),
			Source: &fakeSource{err: errors.New("synthetic provider failure")},
			In:     localTTY,
			Out:    &out,
			Err:    &errOut,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "digits are visible") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "digits are visible") {
		t.Fatalf("manual prompt not shown: stdout = %q, stderr = %q", out.String(), errOut.String())
	}
	if _, err := localMaster.Write([]byte("invalid\n135790\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.ExitCode != 0 || result.Err != nil {
			t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
		}
		if !strings.Contains(out.String(), "code must contain only digits") || !strings.Contains(out.String(), "MANUAL_OK") {
			t.Fatalf("stdout = %q", out.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("buffered manual retry timed out: stdout = %q, stderr = %q", out.String(), errOut.String())
	}
}

func TestBrokerReconnectCanWinVisibleManualArbitration(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), `#!/bin/sh
printf 'Enter 6-digit verification code: '
IFS= read code
if [ "$code" = "135790" ]; then
  printf '\nBROKER_OK\n'
  exit 0
fi
exit 6
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	localMaster, localTTY, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer localMaster.Close()
	defer localTTY.Close()
	source := &reconnectingSource{codeReady: make(chan struct{}), canceled: make(chan struct{})}
	var out safeBuffer
	var errOut safeBuffer
	done := make(chan Result, 1)
	go func() {
		done <- Connect(context.Background(), Options{
			Target: syntheticTarget(),
			Source: source,
			In:     localTTY,
			Out:    &out,
			Err:    &errOut,
		})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "digits are visible") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "digits are visible") {
		t.Fatalf("manual prompt not shown: %q", out.String())
	}
	close(source.codeReady)
	select {
	case result := <-done:
		if result.ExitCode != 0 || !strings.Contains(out.String(), "BROKER_OK") {
			t.Fatalf("result = %+v, stdout = %q, stderr = %q", result, out.String(), errOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("broker reconnect timed out")
	}
}

// TestStaleInputReaderRetiredBeforeNextAttempt guards against a supervisor
// reusing the same *os.File across consecutive Connect calls: if the reader
// goroutine started by the first attempt were still blocked in Read when the
// second attempt starts its own reader, both would compete for bytes on the
// shared TTY. It asserts, after each Connect call returns, that no
// readInputChunks goroutine from a prior attempt remains on the stack.
func TestStaleInputReaderRetiredBeforeNextAttempt(t *testing.T) {
	bin := t.TempDir()
	writeScript(t, filepath.Join(bin, "ssh"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	localMaster, localTTY, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer localMaster.Close()
	defer localTTY.Close()

	for attempt := 1; attempt <= 2; attempt++ {
		var out, errOut safeBuffer
		result := Connect(context.Background(), Options{
			Target: syntheticTarget(),
			Source: &fakeSource{code: "246810"},
			In:     localTTY,
			Out:    &out,
			Err:    &errOut,
		})
		if result.ExitCode != 0 {
			t.Fatalf("attempt %d: result = %+v, stderr = %q", attempt, result, errOut.String())
		}
		assertNoStaleInputReader(t, attempt)
	}
}

// assertNoStaleInputReader polls briefly rather than checking once to absorb
// any scheduler latency between the retiring goroutine's exit and the
// runtime's bookkeeping catching up; the retirement itself is synchronous
// (Connect blocks on it before returning), so this is a safety margin on the
// observation, not on the property being verified.
func assertNoStaleInputReader(t *testing.T, attempt int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if count := countReadInputChunksGoroutines(); count == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("attempt %d: a readInputChunks goroutine from a prior attempt is still running", attempt)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func countReadInputChunksGoroutines() int {
	buffer := make([]byte, 1<<20)
	length := runtime.Stack(buffer, true)
	return strings.Count(string(buffer[:length]), "readInputChunks(")
}

func syntheticTarget() config.EffectiveTarget {
	return config.EffectiveTarget{
		Profile:  "production",
		Target:   "app-01",
		SSH:      "example-one",
		Launcher: "ssh",
		Provider: "bitwarden",
		Item:     "Example Login",
		Fallback: "prompt",
		MFA:      config.MFA{Preset: "jumpserver-koko"},
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}

type safeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *safeBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(value)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
