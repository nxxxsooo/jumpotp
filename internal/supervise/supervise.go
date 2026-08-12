// Package supervise drives the __target wrapper's connect-retry loop: gate
// check, attempt, backoff, repeat, until an operator-initiated stop ends it.
// See openspec/changes/stabilize-workspace-masters/design.md, "D2:
// Supervised reconnection in the target wrapper".
package supervise

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ConnectResult is the outcome of one supervised connection attempt.
type ConnectResult struct {
	ExitCode int
	Err      error
}

// ConnectFunc performs a single connection attempt. Implementations must
// return once ctx is canceled, in addition to returning when the process
// they manage exits on its own; the supervisor relies on this distinction to
// tell an operator stop apart from a child-initiated exit that should
// reconnect.
type ConnectFunc func(ctx context.Context) ConnectResult

// GateFunc reports whether a dial is currently permitted, e.g. whether a
// ControlMaster is already reusable or a broker is validated and reachable.
// An error is treated the same as a false result: the gate stays closed.
type GateFunc func() (bool, error)

// clearScreenSequence homes the cursor, erases the visible screen, then
// erases the terminal's scrollback, in that order: homing first anchors the
// erases that follow to a known position, and the display erase (2J) is
// issued before the scrollback erase (3J) since some terminals interpret 3J
// relative to what is already on screen. Written to the wrapper's own
// output stream only once MasterCheck first confirms the ControlMaster is
// established -- never at the moment a code is submitted, because the
// endpoint's echo of that code is a network round trip and is not
// guaranteed to have arrived by then. Confirmed establishment is the first
// point at which the whole authentication exchange, including any echo, is
// certain to have already landed, so clearing there (immediately before the
// establishment line) replaces it for good. An attempt that never confirms
// a master never clears, so its failure evidence stays on screen. This is a
// terminal control sequence on JumpOTP's own output stream, never a tmux
// mechanism.
const clearScreenSequence = "\033[H\033[2J\033[3J"

const (
	// InitialBackoff is the delay before the first reconnect attempt after a
	// child-initiated exit.
	InitialBackoff = 5 * time.Second
	// MaxBackoff caps the exponential backoff.
	MaxBackoff = 300 * time.Second
	// GatePollInterval is how often the reconnect gate is re-checked while
	// closed. No jitter applies to gate polling, only to reconnect backoff.
	GatePollInterval = 10 * time.Second
	// EstablishProbeInterval is how often, while an attempt is running, the
	// wrapper re-checks MasterCheck to catch the moment a sessionless
	// connection's ControlMaster becomes established. Kept shorter than
	// GatePollInterval so the first confirmation is reported promptly.
	EstablishProbeInterval = 2 * time.Second
	// StableConnection is how long an attempt must run before its end resets
	// backoff back to InitialBackoff instead of continuing to double. Chosen
	// conservatively; neither design.md nor spec.md pins an exact figure for
	// this "reasonable interval" (open concern, see task hand-off notes).
	StableConnection = time.Minute

	// jitterFraction bounds backoff jitter to +/-20% of the nominal delay.
	jitterFraction = 0.2
	// waitLogInterval bounds how often the "waiting for gate" status line
	// repeats while the gate stays closed, beyond the first occurrence.
	waitLogInterval = time.Minute
)

// Supervisor drives Connect in a gated, backed-off retry loop. It carries no
// mutable state, so a value is safe to build once and run with Run(ctx);
// this mirrors the probes.Scheduler{...}.Run(ctx) idiom used elsewhere in
// this repository.
type Supervisor struct {
	// Profile and Target label status lines. They must be the configured
	// profile/target names, never the SSH alias or a Bitwarden item
	// reference, neither of which may appear in supervisor output.
	Profile string
	Target  string
	// Alias is the target's SSH alias, printed in the establishment status
	// line as what to run for interactive work (e.g. "ssh <Alias>"). It
	// must be the resolved SSH alias, never a Bitwarden item reference.
	Alias string

	// Connect performs one connection attempt. Required.
	Connect ConnectFunc
	// MasterCheck reports whether ssh -O check succeeds for the target's
	// alias (a reusable ControlMaster already exists). May be nil.
	MasterCheck GateFunc
	// BrokerActive reports whether the profile's broker is validated and
	// reachable. May be nil.
	BrokerActive GateFunc

	// Out receives redacted connect/exit/backoff/waiting status lines. nil
	// discards them.
	Out io.Writer

	// Now, Sleep, and Rand are test seams. nil defaults to time.Now, a
	// context-aware timer sleep, and math/rand.Float64 respectively.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) bool
	Rand  func() float64

	// Signals is a test seam for the operator-stop signal channel. nil means
	// Run registers its own OS signal.Notify for SIGTERM, SIGHUP, and
	// os.Interrupt, and unregisters it before returning.
	Signals chan os.Signal
}

// Run drives the supervision loop until an operator stop signal arrives (or,
// in tests, a Sleep/gate seam reports one) or the parent ctx ends. It
// returns the last attempt's ConnectResult, or a zero-value ConnectResult
// (exit code 0) if the stop happened while waiting between attempts rather
// than mid-attempt.
func (s Supervisor) Run(ctx context.Context) ConnectResult {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	sleep := s.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	randFloat := s.Rand
	if randFloat == nil {
		randFloat = rand.Float64
	}
	out := s.Out
	if out == nil {
		out = io.Discard
	}

	stopCtx, stopCancel := context.WithCancel(ctx)
	defer stopCancel()

	signals := s.Signals
	if signals == nil {
		signals = make(chan os.Signal, 4)
		signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, os.Interrupt)
		defer signal.Stop(signals)
	}
	// A single long-lived watcher, rather than one per attempt, avoids
	// spawning a fresh goroutine (and the shared-mutable-state races that
	// would invite) on every reconnect.
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-signals:
			stopCancel()
		case <-stopCtx.Done():
		}
	}()
	defer func() {
		stopCancel()
		<-watcherDone
	}()

	backoff := InitialBackoff
	var last ConnectResult
	for {
		if stopCtx.Err() != nil {
			return ConnectResult{}
		}
		if !s.waitForGate(stopCtx, now, sleep, out) {
			return ConnectResult{}
		}
		if stopCtx.Err() != nil {
			return ConnectResult{}
		}
		s.logf(out, "connecting")
		attemptStart := now()
		result := s.runAttempt(stopCtx, sleep, out)
		last = result
		if stopCtx.Err() != nil {
			// Operator stop ended the in-flight attempt; Connect's own
			// ctx handling is responsible for shutting the child down, and
			// its result (not a synthesized one) is what we report.
			return last
		}
		at := now()
		// The delay before this reconnect uses the backoff level this attempt
		// entered with; whether this attempt was stable only decides what
		// backoff level the *next* round starts from, computed after the
		// delay so a long-lived attempt's own reconnect isn't instant.
		delay := applyJitter(backoff, randFloat())
		s.logExit(out, result, delay, at)
		if !sleep(stopCtx, delay) {
			return ConnectResult{}
		}
		if at.Sub(attemptStart) >= StableConnection {
			backoff = InitialBackoff
		} else {
			backoff = nextBackoff(backoff)
		}
	}
}

// runAttempt runs Connect for one attempt while a concurrent goroutine
// probes MasterCheck on EstablishProbeInterval to report the moment a
// ControlMaster becomes established. The probe is always canceled and
// joined before this returns -- whether Connect ended because ctx was
// canceled or because the child process exited on its own -- so it never
// outlives the attempt and its writes to out never race the caller's next
// status line.
func (s Supervisor) runAttempt(ctx context.Context, sleep func(context.Context, time.Duration) bool, out io.Writer) ConnectResult {
	if s.MasterCheck == nil {
		return s.Connect(ctx)
	}
	probeCtx, probeCancel := context.WithCancel(ctx)
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		s.runEstablishProbe(probeCtx, sleep, out)
	}()
	result := s.Connect(ctx)
	probeCancel()
	<-probeDone
	return result
}

// runEstablishProbe re-checks MasterCheck on EstablishProbeInterval while an
// attempt is in flight. On first confirmation it clears the screen (see
// clearScreenSequence) and then prints the establishment line -- in that
// order, so the line is what survives as visible content -- and returns
// immediately after, so it can never report (or clear) more than once per
// attempt; a fresh probe goroutine runs for every attempt, so a later
// attempt reports again. Establishment is read only from MasterCheck, never
// inferred from remote output.
func (s Supervisor) runEstablishProbe(ctx context.Context, sleep func(context.Context, time.Duration) bool, out io.Writer) {
	for {
		if ctx.Err() != nil {
			return
		}
		if s.masterConfirmed() {
			io.WriteString(out, clearScreenSequence)
			s.logf(out, "master established; interactive work goes through ssh %s", s.Alias)
			return
		}
		if !sleep(ctx, EstablishProbeInterval) {
			return
		}
	}
}

// waitForGate blocks until MasterCheck or BrokerActive reports the gate
// open, or ctx ends. It never calls Connect and, while the gate is closed,
// makes no launcher/ssh connection calls of its own -- only the gate probes
// themselves run.
func (s Supervisor) waitForGate(ctx context.Context, now func() time.Time, sleep func(context.Context, time.Duration) bool, out io.Writer) bool {
	var lastLogged time.Time
	logged := false
	for {
		if ctx.Err() != nil {
			return false
		}
		if s.gateOpen() {
			return true
		}
		current := now()
		if !logged || current.Sub(lastLogged) >= waitLogInterval {
			s.logf(out, "waiting for a reusable master or an active broker")
			lastLogged = current
			logged = true
		}
		if !sleep(ctx, GatePollInterval) {
			return false
		}
	}
}

// gateOpen reports whether a dial is currently permitted. A successful
// MasterCheck short-circuits: BrokerActive is not consulted when a reusable
// master already answers.
func (s Supervisor) gateOpen() bool {
	if s.masterConfirmed() {
		return true
	}
	if s.BrokerActive != nil {
		if ok, _ := s.BrokerActive(); ok {
			return true
		}
	}
	return false
}

// masterConfirmed reports whether MasterCheck currently confirms a reusable
// ControlMaster. A nil MasterCheck, or a check that errors, both count as
// not confirmed.
func (s Supervisor) masterConfirmed() bool {
	if s.MasterCheck == nil {
		return false
	}
	ok, _ := s.MasterCheck()
	return ok
}

func (s Supervisor) label() string {
	return fmt.Sprintf("%s/%s", s.Profile, s.Target)
}

func (s Supervisor) logf(out io.Writer, format string, args ...any) {
	fmt.Fprintf(out, "jumpotp: [%s] "+format+"\n", append([]any{s.label()}, args...)...)
}

func (s Supervisor) logExit(out io.Writer, result ConnectResult, delay time.Duration, at time.Time) {
	s.logf(out, "connection ended at %s (exit code %d); reconnecting in %s", at.UTC().Format(time.RFC3339), result.ExitCode, roundSeconds(delay))
}

// nextBackoff doubles current, capped at MaxBackoff.
func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > MaxBackoff || next <= 0 {
		next = MaxBackoff
	}
	return next
}

// applyJitter scales base by a factor in [1-jitterFraction, 1+jitterFraction]
// using r drawn from [0, 1).
func applyJitter(base time.Duration, r float64) time.Duration {
	offset := (2*r - 1) * jitterFraction
	jittered := time.Duration(float64(base) * (1 + offset))
	if jittered < 0 {
		return 0
	}
	return jittered
}

func roundSeconds(d time.Duration) time.Duration {
	return d.Round(time.Second)
}

// sleepContext is the default Sleep seam: it waits for d or ctx.Done(),
// whichever comes first, and reports whether the wait completed normally.
func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
