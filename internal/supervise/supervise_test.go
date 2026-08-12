package supervise

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// stepClock returns each configured time in order, then repeats the last
// entry for any further calls. Repeating (rather than panicking, as
// sequenceClock does in internal/provider/provider_test.go) keeps tests
// robust to a Run loop that keeps going a little longer than the exact
// number of steps a test cares about.
type stepClock struct {
	mu    sync.Mutex
	steps []time.Time
	index int
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.steps[c.index]
	if c.index < len(c.steps)-1 {
		c.index++
	}
	return value
}

// scriptedSleep is the injectable Sleep seam for tests: it never actually
// waits, so backoff schedules spanning minutes run instantly. It records
// every requested duration and, optionally, runs a hook before deciding
// whether to report the wait as completed (true) or stopped (false).
type scriptedSleep struct {
	mu     sync.Mutex
	calls  []time.Duration
	stopAt int // 0 means never auto-stop
	onCall func(call int, d time.Duration)
}

func (s *scriptedSleep) sleep(ctx context.Context, d time.Duration) bool {
	s.mu.Lock()
	s.calls = append(s.calls, d)
	call := len(s.calls)
	s.mu.Unlock()
	if s.onCall != nil {
		s.onCall(call, d)
	}
	if ctx.Err() != nil {
		return false
	}
	if s.stopAt > 0 && call >= s.stopAt {
		return false
	}
	return true
}

func (s *scriptedSleep) snapshot() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.calls...)
}

// fakeConnect records every attempt and delegates to fn for the result.
type fakeConnect struct {
	mu    sync.Mutex
	calls int
	fn    func(ctx context.Context, call int) ConnectResult
}

func (f *fakeConnect) connect(ctx context.Context, call int) ConnectResult {
	if f.fn != nil {
		return f.fn(ctx, call)
	}
	return ConnectResult{}
}

func (f *fakeConnect) Connect(ctx context.Context) ConnectResult {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	return f.connect(ctx, call)
}

func (f *fakeConnect) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// countingGate reports open once its call count reaches openAfter (0 means
// never opens).
type countingGate struct {
	mu        sync.Mutex
	calls     int
	openAfter int
}

func (g *countingGate) check() (bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.openAfter > 0 && g.calls >= g.openAfter {
		return true, nil
	}
	return false, nil
}

func (g *countingGate) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

func alwaysOpen() (bool, error)   { return true, nil }
func alwaysClosed() (bool, error) { return false, nil }

func fixedNow(instant time.Time) func() time.Time {
	return func() time.Time { return instant }
}

func TestBackoffScheduleDoublesWithJitterCapAndPollInterval(t *testing.T) {
	connect := &fakeConnect{fn: func(context.Context, int) ConnectResult {
		return ConnectResult{ExitCode: 1}
	}}
	sleeper := &scriptedSleep{stopAt: 8}
	instant := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sup := Supervisor{
		Profile:     "production",
		Target:      "app-01",
		Connect:     connect.Connect,
		MasterCheck: alwaysOpen,
		Now:         fixedNow(instant), // elapsed is always 0: never a stable-connection reset
		Sleep:       sleeper.sleep,
		Rand:        func() float64 { return 0.5 }, // midpoint: no jitter offset
	}
	sup.Run(context.Background())

	want := []time.Duration{5, 10, 20, 40, 80, 160, 300, 300}
	got := sleeper.snapshot()
	if len(got) != len(want) {
		t.Fatalf("sleep calls = %v, want %d entries matching %v", got, len(want), want)
	}
	for i, w := range want {
		if got[i] != w*time.Second {
			t.Fatalf("sleep[%d] = %v, want %v (full schedule: %v)", i, got[i], w*time.Second, got)
		}
	}
	if connect.callCount() != len(want) {
		t.Fatalf("connect calls = %d, want %d", connect.callCount(), len(want))
	}
}

func TestApplyJitterStaysWithinTwentyPercentBounds(t *testing.T) {
	base := 100 * time.Second
	tests := []struct {
		name string
		r    float64
		want time.Duration
	}{
		{"lower bound at r=0", 0, 80 * time.Second},
		{"midpoint at r=0.5", 0.5, 100 * time.Second},
		{"near upper bound at r just under 1", 0.9995, 119980000 * time.Microsecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := applyJitter(base, test.r)
			if got < 80*time.Second || got > 120*time.Second {
				t.Fatalf("applyJitter(%v, %v) = %v, out of +/-20%% bounds", base, test.r, got)
			}
			diff := got - test.want
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Millisecond {
				t.Fatalf("applyJitter(%v, %v) = %v, want approximately %v", base, test.r, got, test.want)
			}
		})
	}
}

func TestBackoffResetsAfterStableConnection(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Two short (non-stable) failures double the backoff to 20s, then one
	// long (stable) attempt should reset it back to the 5s starting point
	// for the next wait, rather than continuing to 40s.
	clock := &stepClock{steps: []time.Time{
		base, base.Add(1 * time.Second), // attempt 1: short -> no reset, backoff becomes 10s
		base, base.Add(1 * time.Second), // attempt 2: short -> no reset, backoff becomes 20s
		base, base.Add(StableConnection + time.Second), // attempt 3: stable -> reset to 5s
		base, base.Add(1 * time.Second), // attempt 4: observe the reset delay
	}}
	connect := &fakeConnect{fn: func(context.Context, int) ConnectResult { return ConnectResult{} }}
	sleeper := &scriptedSleep{stopAt: 4}
	sup := Supervisor{
		Connect:     connect.Connect,
		MasterCheck: alwaysOpen,
		Now:         clock.Now,
		Sleep:       sleeper.sleep,
		Rand:        func() float64 { return 0.5 },
	}
	sup.Run(context.Background())

	got := sleeper.snapshot()
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 5 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("sleep calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sleep[%d] = %v, want %v (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestGateClosedPollsWithoutConnecting(t *testing.T) {
	connect := &fakeConnect{}
	sleeper := &scriptedSleep{stopAt: 5}
	sup := Supervisor{
		Connect:      connect.Connect,
		MasterCheck:  alwaysClosed,
		BrokerActive: alwaysClosed,
		Now:          fixedNow(time.Now()),
		Sleep:        sleeper.sleep,
	}
	sup.Run(context.Background())

	if connect.callCount() != 0 {
		t.Fatalf("connect calls = %d, want 0 while the gate never opens", connect.callCount())
	}
	got := sleeper.snapshot()
	if len(got) != 5 {
		t.Fatalf("gate poll count = %d, want 5", len(got))
	}
	for i, d := range got {
		if d != GatePollInterval {
			t.Fatalf("sleep[%d] = %v, want the fixed %v gate poll interval", i, d, GatePollInterval)
		}
	}
}

func TestGateOpensImmediatelyReconnects(t *testing.T) {
	broker := &countingGate{openAfter: 4}
	connect := &fakeConnect{fn: func(context.Context, int) ConnectResult { return ConnectResult{} }}
	// 3 closed-gate polls precede the 4th (open) check, so the post-connect
	// backoff wait is the 4th Sleep call; stop right there.
	sleeper := &scriptedSleep{stopAt: 4}
	sup := Supervisor{
		MasterCheck:  alwaysClosed,
		BrokerActive: broker.check,
		Connect:      connect.Connect,
		Now:          fixedNow(time.Now()),
		Sleep:        sleeper.sleep,
	}
	sup.Run(context.Background())

	if broker.callCount() != 4 {
		t.Fatalf("broker gate checks = %d, want exactly 4 (opens on the 4th)", broker.callCount())
	}
	if connect.callCount() != 1 {
		t.Fatalf("connect calls = %d, want exactly 1 right after the gate opened", connect.callCount())
	}
}

func TestMasterCheckSuccessSkipsBrokerActive(t *testing.T) {
	broker := &countingGate{}
	connect := &fakeConnect{fn: func(context.Context, int) ConnectResult { return ConnectResult{} }}
	sleeper := &scriptedSleep{stopAt: 1}
	sup := Supervisor{
		MasterCheck:  alwaysOpen,
		BrokerActive: broker.check,
		Connect:      connect.Connect,
		Now:          fixedNow(time.Now()),
		Sleep:        sleeper.sleep,
	}
	sup.Run(context.Background())

	if broker.callCount() != 0 {
		t.Fatalf("broker gate checks = %d, want 0: a successful master check must short-circuit it", broker.callCount())
	}
	if connect.callCount() == 0 {
		t.Fatal("connect was never called despite an open master-check gate")
	}
}

func TestSignalDuringWaitEndsWithoutRespawn(t *testing.T) {
	tests := []struct {
		name   string
		signal os.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGHUP", syscall.SIGHUP},
		{"interrupt", os.Interrupt},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connect := &fakeConnect{}
			signals := make(chan os.Signal, 1)
			sleeper := &scriptedSleep{onCall: func(call int, _ time.Duration) {
				if call == 1 {
					signals <- test.signal
				}
			}}
			sup := Supervisor{
				Connect:      connect.Connect,
				MasterCheck:  alwaysClosed,
				BrokerActive: alwaysClosed,
				Now:          fixedNow(time.Now()),
				Sleep:        sleeper.sleep,
				Signals:      signals,
			}
			result := sup.Run(context.Background())

			if connect.callCount() != 0 {
				t.Fatalf("connect calls = %d, want 0: the gate never opened before the stop signal", connect.callCount())
			}
			if result != (ConnectResult{}) {
				t.Fatalf("result = %+v, want the zero value for a stop while waiting", result)
			}
		})
	}
}

func TestSignalDuringAttemptEndsWithoutRespawn(t *testing.T) {
	tests := []struct {
		name   string
		signal os.Signal
	}{
		{"SIGTERM", syscall.SIGTERM},
		{"SIGHUP", syscall.SIGHUP},
		{"interrupt", os.Interrupt},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connect := &fakeConnect{}
			signals := make(chan os.Signal, 1)
			connect.fn = func(ctx context.Context, call int) ConnectResult {
				signals <- test.signal
				<-ctx.Done()
				return ConnectResult{ExitCode: 130, Err: context.Canceled}
			}
			sup := Supervisor{
				Connect:     connect.Connect,
				MasterCheck: alwaysOpen,
				Now:         fixedNow(time.Now()),
				Sleep:       (&scriptedSleep{}).sleep,
				Signals:     signals,
			}
			result := sup.Run(context.Background())

			if connect.callCount() != 1 {
				t.Fatalf("connect calls = %d, want exactly 1: no respawn after an operator stop", connect.callCount())
			}
			if result.ExitCode != 130 {
				t.Fatalf("result = %+v, want the in-flight attempt's own exit code", result)
			}
		})
	}
}

func TestPerAttemptFreshState(t *testing.T) {
	var seenCalls []int
	var mu sync.Mutex
	connect := &fakeConnect{fn: func(_ context.Context, call int) ConnectResult {
		mu.Lock()
		seenCalls = append(seenCalls, call)
		mu.Unlock()
		return ConnectResult{ExitCode: 1}
	}}
	sleeper := &scriptedSleep{stopAt: 3}
	sup := Supervisor{
		Connect:     connect.Connect,
		MasterCheck: alwaysOpen,
		Now:         fixedNow(time.Now()),
		Sleep:       sleeper.sleep,
	}
	sup.Run(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(seenCalls) != 3 {
		t.Fatalf("attempts observed = %v, want 3 independent calls", seenCalls)
	}
	for i, call := range seenCalls {
		if call != i+1 {
			t.Fatalf("attempts observed = %v, want each call numbered fresh in order", seenCalls)
		}
	}
}

func TestStatusLinesAreRedacted(t *testing.T) {
	const secretCode = "000000"
	const secretItem = "Example Bitwarden Item"
	connect := &fakeConnect{fn: func(context.Context, int) ConnectResult {
		return ConnectResult{ExitCode: 1, Err: errors.New("synthetic failure")}
	}}
	sleeper := &scriptedSleep{stopAt: 2}
	var out strings.Builder
	sup := Supervisor{
		Profile:      "production",
		Target:       "app-01",
		Connect:      connect.Connect,
		MasterCheck:  alwaysClosed,
		BrokerActive: alwaysClosed,
		Now:          fixedNow(time.Now()),
		Sleep:        sleeper.sleep,
		Out:          &out,
	}
	sup.Run(context.Background())

	output := out.String()
	if !strings.Contains(output, "production/app-01") {
		t.Fatalf("status lines missing the profile/target label: %q", output)
	}
	if strings.Contains(output, secretCode) {
		t.Fatalf("status lines leaked a code-shaped value: %q", output)
	}
	if strings.Contains(output, secretItem) || strings.Contains(strings.ToLower(output), "item") {
		t.Fatalf("status lines leaked an item reference: %q", output)
	}
	for _, token := range []string{"-N", "-o", "ServerAliveInterval", "ControlPersist", "example-one"} {
		if strings.Contains(output, token) {
			t.Fatalf("status lines leaked a raw ssh argument %q: %q", token, output)
		}
	}
	if !strings.Contains(output, "waiting for a reusable master") {
		t.Fatalf("status lines missing the waiting state: %q", output)
	}
}

func TestConnectingAndExitStatusLinesReportTimestampAndDelay(t *testing.T) {
	connect := &fakeConnect{fn: func(context.Context, int) ConnectResult {
		return ConnectResult{ExitCode: 3}
	}}
	sleeper := &scriptedSleep{stopAt: 1}
	var out strings.Builder
	sup := Supervisor{
		Profile:     "production",
		Target:      "app-01",
		Connect:     connect.Connect,
		MasterCheck: alwaysOpen,
		Now:         fixedNow(time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)),
		Sleep:       sleeper.sleep,
		Rand:        func() float64 { return 0.5 },
		Out:         &out,
	}
	sup.Run(context.Background())

	output := out.String()
	if !strings.Contains(output, "connecting") {
		t.Fatalf("missing the connect status line: %q", output)
	}
	if !strings.Contains(output, "exit code 3") {
		t.Fatalf("missing the exit code in the exit status line: %q", output)
	}
	if !strings.Contains(output, "2026-03-04T05:06:07Z") {
		t.Fatalf("missing the exit timestamp: %q", output)
	}
	if !strings.Contains(output, "reconnecting in 5s") {
		t.Fatalf("missing the next backoff delay: %q", output)
	}
}

func TestRunReturnsZeroValueWhenAlreadyStoppedBeforeFirstAttempt(t *testing.T) {
	connect := &fakeConnect{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sup := Supervisor{
		Connect:     connect.Connect,
		MasterCheck: alwaysOpen,
		Now:         fixedNow(time.Now()),
		Sleep:       (&scriptedSleep{}).sleep,
		Signals:     make(chan os.Signal, 1),
	}
	result := sup.Run(ctx)
	if connect.callCount() != 0 {
		t.Fatalf("connect calls = %d, want 0 when already stopped before the first attempt", connect.callCount())
	}
	if result != (ConnectResult{}) {
		t.Fatalf("result = %+v, want the zero value", result)
	}
}

func TestNextBackoffDoublesAndCaps(t *testing.T) {
	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{5 * time.Second, 10 * time.Second},
		{160 * time.Second, 300 * time.Second},
		{300 * time.Second, 300 * time.Second},
	}
	for _, test := range tests {
		if got := nextBackoff(test.current); got != test.want {
			t.Fatalf("nextBackoff(%v) = %v, want %v", test.current, got, test.want)
		}
	}
}

func TestSleepContextReportsStopOnCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepContext(ctx, time.Minute) {
		t.Fatal("sleepContext returned true for an already-canceled context")
	}
}

func TestSleepContextCompletesForAShortDuration(t *testing.T) {
	if !sleepContext(context.Background(), time.Millisecond) {
		t.Fatal("sleepContext returned false despite an uncancceled context and a tiny duration")
	}
}
