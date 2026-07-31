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
	"syscall"

	"github.com/creack/pty"
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
	code []byte
	err  error
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
	inputCh := make(chan []byte, 16)
	inputErrCh := make(chan error, 1)
	waitCh := make(chan error, 1)
	providerCh := make(chan providerResult, 1)

	go readChunks(ptmx, outputCh, outputErrCh)
	go readChunks(options.In, inputCh, inputErrCh)
	go func() { waitCh <- command.Wait() }()

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGWINCH, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	autoUsed := false
	providerPending := false
	var providerCancel context.CancelFunc
	defer func() {
		if providerCancel != nil {
			providerCancel()
		}
	}()
	manualMode := false
	manualBuffer := make([]byte, 0, 16)
	interrupted := false
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
		fmt.Fprintf(options.Out, "\r\nJumpOTP: %s. Enter the code manually (digits are visible): ", reason)
		manualMode = true
		manualBuffer = manualBuffer[:0]
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
		case value := <-outputCh:
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
			if options.Target.Manual || autoUsed {
				reason := "manual mode requested"
				if autoUsed {
					reason = "automatic submission was already used for this connection"
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
			providerPending = true
			go func(item string) {
				code, codeErr := options.Source.Code(ctx, item)
				providerCh <- providerResult{code: code, err: codeErr}
			}(options.Target.Item)
		case result := <-providerCh:
			providerPending = false
			if result.err != nil {
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
						providerPending = true
						go func(item string) {
							code, codeErr := awaiting.AwaitCode(awaitCtx, item)
							providerCh <- providerResult{code: code, err: codeErr}
						}(options.Target.Item)
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
				if fallbackErr := enterManual("Bitwarden returned an invalid TOTP value"); fallbackErr != nil {
					finish()
					return Result{ExitCode: 4, Err: fallbackErr}
				}
				continue
			}
			if manualMode {
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
			payload := append(result.code, '\n')
			_, writeErr := ptmx.Write(payload)
			mfa.Zero(payload)
			mfa.Zero(result.code)
			if writeErr != nil {
				finish()
				return Result{ExitCode: 4, Err: fmt.Errorf("submit automatic code: %w", writeErr)}
			}
			autoUsed = true
			matcher.Reset()
		case value := <-inputCh:
			if len(value) == 0 {
				continue
			}
			if !manualMode {
				if _, err := ptmx.Write(value); err != nil {
					return Result{ExitCode: childExitCode(command.ProcessState), Err: err}
				}
				continue
			}
			manualBuffer = append(manualBuffer, value...)
			if len(manualBuffer) > 64 {
				fmt.Fprint(options.Out, "\r\nJumpOTP: manual code is too long. Try again: ")
				manualBuffer = manualBuffer[:0]
				continue
			}
			lineEnd := bytes.IndexAny(manualBuffer, "\r\n")
			if lineEnd < 0 {
				continue
			}
			code := append([]byte(nil), manualBuffer[:lineEnd]...)
			remainderStart := lineEnd + 1
			if manualBuffer[lineEnd] == '\r' && remainderStart < len(manualBuffer) && manualBuffer[remainderStart] == '\n' {
				remainderStart++
			}
			remainder := append([]byte(nil), manualBuffer[remainderStart:]...)
			manualBuffer = manualBuffer[:0]
			if err := mfa.ValidateCode(code, options.Target.MFA); err != nil {
				mfa.Zero(code)
				fmt.Fprintf(options.Out, "\r\nJumpOTP: %s. Try again: ", err)
				continue
			}
			if providerCancel != nil {
				providerCancel()
				providerCancel = nil
				providerPending = false
			}
			payload := append(code, '\n')
			_, writeErr := ptmx.Write(payload)
			mfa.Zero(payload)
			mfa.Zero(code)
			if writeErr != nil {
				finish()
				return Result{ExitCode: 4, Err: fmt.Errorf("submit manual code: %w", writeErr)}
			}
			manualMode = false
			autoUsed = true
			matcher.Reset()
			if isTTY {
				if _, err := term.MakeRaw(fd); err != nil {
					finish()
					return Result{ExitCode: 4, Err: fmt.Errorf("restore proxy terminal mode: %w", err)}
				}
				rawActive = true
			}
			if len(remainder) > 0 {
				_, _ = ptmx.Write(remainder)
				mfa.Zero(remainder)
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
	}
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
