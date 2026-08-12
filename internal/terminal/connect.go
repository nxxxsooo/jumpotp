package terminal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/nxxxsooo/jumpotp/internal/config"
	"github.com/nxxxsooo/jumpotp/internal/launcher"
	"github.com/nxxxsooo/jumpotp/internal/mfa"
	"github.com/nxxxsooo/jumpotp/internal/provider"
)

type Options struct {
	Target config.EffectiveTarget
	Source provider.Source
	In     *os.File
	Out    io.Writer
	Err    io.Writer
}

type Result struct {
	ExitCode int
	Err      error
}

type providerResult struct {
	generation uint64
	code       []byte
	err        error
}

type terminalInputMode int32

const (
	inputProxy terminalInputMode = iota
	inputSuppressed
	inputManual
	maxManualCodeBytes = 64
)

type terminalInput struct {
	value []byte
	mode  terminalInputMode
}

// totpWindowPeriod is the epoch-aligned TOTP window used by the
// jumpserver-koko preset (6-digit/30s), matching the broker's rotation
// guard. boundarySkew absorbs clock/RTT slop so a retry fetched right at the
// boundary has not itself gone stale by the time it reaches the endpoint.
const (
	totpWindowPeriod = 30 * time.Second
	boundarySkew     = 300 * time.Millisecond
)

// nowFunc and waitUntilBoundary are the clock seam for the bounded
// resubmission retry below; tests override both to avoid real 30s waits.
var nowFunc = time.Now

var waitUntilBoundary = func(ctx context.Context, target time.Time) error {
	delay := target.Sub(nowFunc())
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// nextTOTPBoundary returns the next epoch-aligned totpWindowPeriod boundary
// strictly after t, even when t already sits exactly on one.
func nextTOTPBoundary(t time.Time) time.Time {
	period := int64(totpWindowPeriod / time.Second)
	seconds := t.Unix()
	boundarySeconds := seconds - seconds%period + period
	return time.Unix(boundarySeconds, 0)
}

// evaluateRetryCode compares a freshly retried code against the previously
// submitted one. previous is always zeroed since this comparison is its
// last use; fresh is zeroed too only on a match, since a match means it
// will never be submitted (on a non-match the caller submits fresh and
// zeroes it itself once written).
func evaluateRetryCode(previous, fresh []byte) bool {
	match := len(previous) > 0 && bytes.Equal(previous, fresh)
	mfa.Zero(previous)
	if match {
		mfa.Zero(fresh)
	}
	return match
}

func Connect(ctx context.Context, options Options) Result {
	if options.In == nil || options.Out == nil || options.Err == nil {
		return Result{ExitCode: 4, Err: errors.New("interactive streams are required")}
	}
	matcher, err := mfa.NewMatcher(options.Target.MFA)
	if err != nil {
		return Result{ExitCode: 2, Err: err}
	}
	command, err := launcher.CommandContext(ctx, options.Target)
	if err != nil {
		return Result{ExitCode: 3, Err: err}
	}
	ptmx, err := pty.Start(command)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Result{ExitCode: 3, Err: fmt.Errorf("%s was not found", options.Target.Launcher)}
		}
		return Result{ExitCode: 3, Err: fmt.Errorf("start launcher: %w", err)}
	}
	defer ptmx.Close()

	fd := int(options.In.Fd())
	// options.In.Fd() (above) permanently disables options.In.SetReadDeadline
	// per os.File.Fd's documented behavior, so readInputChunks below cannot
	// use a deadline to unblock a pending read when a supervisor retires this
	// attempt. Put the fd in non-blocking mode instead so its own poll+read
	// loop can be cancelled without relying on that mechanism.
	_ = unix.SetNonblock(fd, true)
	isTTY := term.IsTerminal(fd)
	var originalState *term.State
	rawActive := false
	if isTTY {
		originalState, err = term.MakeRaw(fd)
		if err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return Result{ExitCode: 4, Err: fmt.Errorf("enter raw terminal mode: %w", err)}
		}
		rawActive = true
		defer func() {
			_ = term.Restore(fd, originalState)
		}()
		_ = pty.InheritSize(options.In, ptmx)
	}

	outputCh := make(chan []byte, 16)
	outputErrCh := make(chan error, 1)
	inputCh := make(chan terminalInput, 16)
	inputErrCh := make(chan error, 1)
	waitCh := make(chan error, 1)
	providerCh := make(chan providerResult, 1)
	var currentInputMode atomic.Int32

	go func() {
		readChunks(ptmx, outputCh, outputErrCh)
		close(outputCh)
	}()
	inputCancel := make(chan struct{})
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		readInputChunks(fd, inputCh, inputErrCh, isTTY, &currentInputMode, inputCancel)
	}()
	// A supervisor may call Connect again with the same *os.File once this
	// attempt ends. Without retiring this goroutine first, it stays parked
	// in readInputChunks and races the next attempt's reader for the same
	// TTY bytes. Closing inputCancel makes the poll-gated loop below exit at
	// its next wakeup, and waiting on inputDone guarantees it has fully
	// stopped reading before this attempt returns.
	defer func() {
		close(inputCancel)
		<-inputDone
	}()
	go func() { waitCh <- command.Wait() }()

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGWINCH, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	autoSubmissions := 0
	manualSubmitted := false
	providerPending := false
	retryFetchPending := false
	var previousCode []byte
	var previousCodeAt time.Time
	defer func() { mfa.Zero(previousCode) }()
	var providerGeneration uint64
	var providerCancel context.CancelFunc
	defer func() {
		if providerCancel != nil {
			providerCancel()
		}
	}()
	manualMode := false
	manualBuffer := make([]byte, 0, 16)
	defer func() { mfa.Zero(manualBuffer) }()
	interrupted := false
	var completed *Result
	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			if command.Process != nil {
				_ = command.Process.Signal(syscall.SIGTERM)
			}
		})
	}
	defer finish()

	enterManual := func(reason string) error {
		if options.Target.Fallback == "fail" {
			return errors.New(reason)
		}
		if !isTTY {
			return errors.New(reason + "; no controlling terminal is available for manual fallback")
		}
		if rawActive {
			if err := term.Restore(fd, originalState); err != nil {
				return fmt.Errorf("restore visible terminal input: %w", err)
			}
			rawActive = false
		}
		currentInputMode.Store(int32(inputSuppressed))
		if err := flushTerminalInput(fd); err != nil {
			return fmt.Errorf("discard input queued before manual fallback: %w", err)
		}
		manualMode = true
		mfa.Zero(manualBuffer)
		manualBuffer = manualBuffer[:0]
		currentInputMode.Store(int32(inputManual))
		fmt.Fprintf(options.Out, "\r\nJumpOTP: %s. Enter the code manually (digits are visible): ", reason)
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			interrupted = true
			finish()
		case sig := <-signals:
			if sig == syscall.SIGWINCH {
				if isTTY {
					_ = pty.InheritSize(options.In, ptmx)
				}
				continue
			}
			if sig == os.Interrupt {
				interrupted = true
			}
			if command.Process != nil {
				_ = command.Process.Signal(sig)
			}
		case value, ok := <-outputCh:
			if !ok {
				outputCh = nil
				if completed != nil {
					return *completed
				}
				continue
			}
			if len(value) == 0 {
				continue
			}
			_, _ = options.Out.Write(value)
			if providerPending || manualMode {
				continue
			}
			if !matcher.Feed(value) {
				continue
			}
			matcher.Reset()
			if options.Target.Manual || manualSubmitted || autoSubmissions >= 2 {
				reason := "manual mode requested"
				switch {
				case manualSubmitted:
					reason = "manual code was already submitted for this connection"
				case autoSubmissions >= 2:
					reason = "automatic submission is exhausted for this connection"
				}
				if err := enterManual(reason); err != nil {
					finish()
					return Result{ExitCode: 4, Err: err}
				}
				continue
			}
			if options.Source == nil {
				if err := enterManual("OTP provider is unavailable"); err != nil {
					finish()
					return Result{ExitCode: 4, Err: err}
				}
				continue
			}
			currentInputMode.Store(int32(inputSuppressed))
			providerGeneration++
			generation := providerGeneration
			providerPending = true
			if autoSubmissions == 1 {
				// Second strict-matched prompt after one automatic
				// submission: the endpoint rejected that code. Wait for the
				// next TOTP window boundary before fetching so the retry is
				// never drawn from the same window as the rejected code.
				retryFetchPending = true
				boundary := nextTOTPBoundary(previousCodeAt).Add(boundarySkew)
				go func(item string, generation uint64, boundary time.Time) {
					if err := waitUntilBoundary(ctx, boundary); err != nil {
						providerCh <- providerResult{generation: generation, err: err}
						return
					}
					code, codeErr := options.Source.Code(ctx, item)
					providerCh <- providerResult{generation: generation, code: code, err: codeErr}
				}(options.Target.Item, generation, boundary)
				continue
			}
			go func(item string, generation uint64) {
				code, codeErr := options.Source.Code(ctx, item)
				providerCh <- providerResult{generation: generation, code: code, err: codeErr}
			}(options.Target.Item, generation)
		case result := <-providerCh:
			if result.generation != providerGeneration || !providerPending {
				mfa.Zero(result.code)
				continue
			}
			providerPending = false
			isRetry := retryFetchPending
			retryFetchPending = false
			if result.err != nil {
				if isRetry {
					mfa.Zero(previousCode)
					previousCode = nil
					if manualMode {
						continue
					}
					if err := enterManual(provider.SafeMessage(result.err)); err != nil {
						finish()
						return Result{ExitCode: 4, Err: err}
					}
					continue
				}
				if awaiting, ok := options.Source.(provider.AwaitingSource); ok && !manualMode {
					kind := provider.KindOf(result.err)
					if kind == provider.Unavailable || kind == provider.TimedOut {
						if err := enterManual(provider.SafeMessage(result.err)); err != nil {
							finish()
							return Result{ExitCode: 4, Err: err}
						}
						var awaitCtx context.Context
						var cancelAwait context.CancelFunc
						awaitCtx, cancelAwait = context.WithCancel(ctx)
						providerCancel = cancelAwait
						defer cancelAwait()
						providerGeneration++
						generation := providerGeneration
						providerPending = true
						go func(item string, generation uint64) {
							code, codeErr := awaiting.AwaitCode(awaitCtx, item)
							providerCh <- providerResult{generation: generation, code: code, err: codeErr}
						}(options.Target.Item, generation)
						continue
					}
				}
				if manualMode {
					continue
				}
				if err := enterManual(provider.SafeMessage(result.err)); err != nil {
					finish()
					return Result{ExitCode: 4, Err: err}
				}
				continue
			}
			if err := mfa.ValidateCode(result.code, options.Target.MFA); err != nil {
				mfa.Zero(result.code)
				if isRetry {
					mfa.Zero(previousCode)
					previousCode = nil
				}
				if fallbackErr := enterManual("Bitwarden returned an invalid TOTP value"); fallbackErr != nil {
					finish()
					return Result{ExitCode: 4, Err: fallbackErr}
				}
				continue
			}
			if isRetry && evaluateRetryCode(previousCode, result.code) {
				previousCode = nil
				if err := enterManual("a fresh TOTP code was not yet available; automatic submission is exhausted for this connection"); err != nil {
					finish()
					return Result{ExitCode: 4, Err: err}
				}
				continue
			}
			if manualMode {
				currentInputMode.Store(int32(inputSuppressed))
				manualMode = false
				mfa.Zero(manualBuffer)
				manualBuffer = manualBuffer[:0]
				if isTTY {
					if _, err := term.MakeRaw(fd); err != nil {
						mfa.Zero(result.code)
						finish()
						return Result{ExitCode: 4, Err: fmt.Errorf("restore proxy terminal mode: %w", err)}
					}
					rawActive = true
				}
			}
			// Retain a copy before the code is zeroed below: if the endpoint
			// rejects this submission, the next matched prompt needs it to
			// recognize (and refuse to resubmit) a stale retry code.
			var retained []byte
			if isRetry {
				previousCode = nil
			} else {
				retained = append([]byte(nil), result.code...)
			}
			payload := append(result.code, '\n')
			_, writeErr := ptmx.Write(payload)
			mfa.Zero(payload)
			mfa.Zero(result.code)
			if writeErr != nil {
				mfa.Zero(retained)
				finish()
				return Result{ExitCode: 4, Err: fmt.Errorf("submit automatic code: %w", writeErr)}
			}
			if isRetry {
				autoSubmissions = 2
			} else {
				autoSubmissions = 1
				previousCode = retained
				previousCodeAt = nowFunc()
			}
			matcher.Reset()
			currentInputMode.Store(int32(inputProxy))
		case input := <-inputCh:
			value := input.value
			if len(value) == 0 {
				continue
			}
			switch input.mode {
			case inputSuppressed:
				if err := forwardPendingControls(ptmx, value); err != nil {
					return Result{ExitCode: childExitCode(command.ProcessState), Err: err}
				}
				continue
			case inputManual:
				if !manualMode {
					mfa.Zero(value)
					continue
				}
			case inputProxy:
				if manualMode || providerPending {
					if err := forwardPendingControls(ptmx, value); err != nil {
						return Result{ExitCode: childExitCode(command.ProcessState), Err: err}
					}
					continue
				}
				if _, err := ptmx.Write(value); err != nil {
					return Result{ExitCode: childExitCode(command.ProcessState), Err: err}
				}
				continue
			default:
				mfa.Zero(value)
				continue
			}
			if !manualMode {
				if _, err := ptmx.Write(value); err != nil {
					return Result{ExitCode: childExitCode(command.ProcessState), Err: err}
				}
				continue
			}
			manualBuffer = append(manualBuffer, value...)
			for manualMode {
				lineEnd := bytes.IndexAny(manualBuffer, "\r\n")
				if lineEnd < 0 {
					if len(manualBuffer) > maxManualCodeBytes {
						mfa.Zero(manualBuffer)
						manualBuffer = manualBuffer[:0]
						fmt.Fprint(options.Out, "\r\nJumpOTP: manual code is too long. Try again: ")
					}
					break
				}
				remainderStart := lineEnd + 1
				if manualBuffer[lineEnd] == '\r' && remainderStart < len(manualBuffer) && manualBuffer[remainderStart] == '\n' {
					remainderStart++
				}
				tooLong := lineEnd > maxManualCodeBytes
				var code []byte
				if !tooLong {
					code = append([]byte(nil), manualBuffer[:lineEnd]...)
				}
				remaining := len(manualBuffer) - remainderStart
				copy(manualBuffer, manualBuffer[remainderStart:])
				mfa.Zero(manualBuffer[remaining:])
				manualBuffer = manualBuffer[:remaining]
				if tooLong {
					fmt.Fprint(options.Out, "\r\nJumpOTP: manual code is too long. Try again: ")
					continue
				}
				if err := mfa.ValidateCode(code, options.Target.MFA); err != nil {
					mfa.Zero(code)
					fmt.Fprintf(options.Out, "\r\nJumpOTP: %s. Try again: ", err)
					continue
				}
				if providerCancel != nil {
					providerGeneration++
					providerCancel()
					providerCancel = nil
					providerPending = false
				}
				currentInputMode.Store(int32(inputSuppressed))
				payload := append(code, '\n')
				_, writeErr := ptmx.Write(payload)
				mfa.Zero(payload)
				mfa.Zero(code)
				if writeErr != nil {
					finish()
					return Result{ExitCode: 4, Err: fmt.Errorf("submit manual code: %w", writeErr)}
				}
				manualMode = false
				manualSubmitted = true
				matcher.Reset()
				if isTTY {
					if _, err := term.MakeRaw(fd); err != nil {
						finish()
						return Result{ExitCode: 4, Err: fmt.Errorf("restore proxy terminal mode: %w", err)}
					}
					rawActive = true
				}
				if len(manualBuffer) > 0 {
					_, _ = ptmx.Write(manualBuffer)
					mfa.Zero(manualBuffer)
					manualBuffer = manualBuffer[:0]
				}
				currentInputMode.Store(int32(inputProxy))
			}
		case readErr := <-outputErrCh:
			if readErr != nil && !errors.Is(readErr, io.EOF) && !isClosedPTY(readErr) {
				fmt.Fprintf(options.Err, "jumpotp: PTY output ended: %v\n", readErr)
			}
		case readErr := <-inputErrCh:
			if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, os.ErrClosed) {
				fmt.Fprintf(options.Err, "jumpotp: terminal input ended: %v\n", readErr)
			}
		case waitErr := <-waitCh:
			result := childResult(waitErr, interrupted)
			completed = &result
			waitCh = nil
			if outputCh == nil {
				return result
			}
		}
	}
}

func childResult(waitErr error, interrupted bool) Result {
	if interrupted {
		return Result{ExitCode: 130, Err: context.Canceled}
	}
	if waitErr == nil {
		return Result{ExitCode: 0}
	}
	var exitError *exec.ExitError
	if errors.As(waitErr, &exitError) {
		return Result{ExitCode: exitError.ExitCode(), Err: waitErr}
	}
	return Result{ExitCode: 4, Err: waitErr}
}

func readChunks(reader io.Reader, values chan<- []byte, errorsOut chan<- error) {
	buffer := make([]byte, 4096)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			value := append([]byte(nil), buffer[:count]...)
			values <- value
		}
		if err != nil {
			errorsOut <- err
			return
		}
	}
}

// inputPollTimeoutMillis bounds how long readInputChunks can sit inside a
// single unix.Poll call, which in turn bounds how quickly it notices cancel
// being closed. It is short enough to retire an idle attempt promptly and
// long enough to keep the poll loop cheap.
const inputPollTimeoutMillis = 200

// readInputChunks reads operator input from fd until it hits a terminal
// error, EOF (once, or twice in a row when retryEOF is set), or cancel is
// closed. It reads via a poll-then-read loop on the raw fd rather than
// file.Read because, by the time this is called, Connect has already called
// options.In.Fd() (for term.IsTerminal/term.MakeRaw), which per os.File.Fd's
// documented behavior permanently disables that File's SetReadDeadline --
// so a blocked file.Read could not be unblocked from outside. Gating each
// read behind a bounded poll lets the loop re-check cancel on its own
// instead, which is what makes it safe for a supervisor to reuse the same
// *os.File across consecutive Connect attempts: this loop is guaranteed to
// have stopped touching fd before Connect returns (see the deferred
// close(inputCancel); <-inputDone in Connect).
func readInputChunks(fd int, values chan<- terminalInput, errorsOut chan<- error, retryEOF bool, mode *atomic.Int32, cancel <-chan struct{}) {
	buffer := make([]byte, 4096)
	previousReadWasEOF := false
	for {
		select {
		case <-cancel:
			return
		default:
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(pollFDs, inputPollTimeoutMillis)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			select {
			case errorsOut <- err:
			case <-cancel:
			}
			return
		}
		if ready == 0 {
			continue
		}
		count, err := unix.Read(fd, buffer)
		if count > 0 {
			value := append([]byte(nil), buffer[:count]...)
			// An unguarded send can block forever once Connect stops draining
			// values, and Connect's deferred <-inputDone would then deadlock;
			// the cancel arm also zeroes bytes that may hold a manual code.
			select {
			case values <- terminalInput{value: value, mode: terminalInputMode(mode.Load())}:
			case <-cancel:
				mfa.Zero(value)
				return
			}
			previousReadWasEOF = false
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
			continue
		}
		if err == nil && count == 0 {
			// A zero-byte, error-free read (e.g. from /dev/null) signals EOF
			// under POSIX read() semantics, matching what os.File.Read would
			// have surfaced as io.EOF.
			err = io.EOF
		} else if err == nil {
			continue
		}
		if retryEOF && errors.Is(err, io.EOF) {
			if previousReadWasEOF {
				select {
				case errorsOut <- err:
				case <-cancel:
				}
				return
			}
			previousReadWasEOF = true
			continue
		}
		select {
		case errorsOut <- err:
		case <-cancel:
		}
		return
	}
}

func forwardPendingControls(writer io.Writer, value []byte) error {
	controls := value[:0]
	for _, current := range value {
		switch current {
		case 0x03, 0x1a, 0x1c:
			controls = append(controls, current)
		}
	}
	var err error
	if len(controls) > 0 {
		_, err = writer.Write(controls)
	}
	mfa.Zero(value)
	return err
}

func childExitCode(state *os.ProcessState) int {
	if state == nil {
		return 4
	}
	return state.ExitCode()
}

func isClosedPTY(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "input/output error") || strings.Contains(message, "file already closed")
}
