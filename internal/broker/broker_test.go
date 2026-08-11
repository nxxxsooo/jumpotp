package broker

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
	"github.com/nxxxsooo/jumpotp/internal/provider"
	runtimepath "github.com/nxxxsooo/jumpotp/internal/runtime"
)

type recordingSource struct {
	mu    sync.Mutex
	code  string
	err   error
	items []string
}

type blockingSource struct {
	started chan struct{}
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (s *blockingSource) Code(ctx context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	s.calls++
	if s.calls == 1 {
		close(s.started)
	}
	s.mu.Unlock()
	select {
	case <-s.release:
		return []byte("246810"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *blockingSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *recordingSource) Code(_ context.Context, item string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, item)
	if s.err != nil {
		return nil, s.err
	}
	return []byte(s.code), nil
}

func (s *recordingSource) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.items...)
}

func TestBrokerGroupsReadyTargetsAndRefreshesLateTarget(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &recordingSource{code: "246810"}
	server, err := NewServer(cfg, "production", source, 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	disableRotationGuard(server)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	defer func() {
		_ = server.Close()
		<-serveDone
	}()

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 2)
	for index, target := range []string{"app-01", "app-01"} {
		client := &Client{Socket: server.Socket(), Profile: "production", Target: target, Nonce: string(rune('a' + index)), Timeout: time.Second}
		go func() {
			code, err := client.Code(context.Background(), "ignored")
			results <- result{code: string(code), err: err}
		}()
	}
	for range 2 {
		got := <-results
		if got.err != nil || got.code != "246810" {
			t.Fatalf("result = %+v", got)
		}
	}
	if got := len(source.calls()); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}

	late := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Nonce: "late", Timeout: time.Second}
	if _, err := late.Code(context.Background(), "ignored"); err != nil {
		t.Fatal(err)
	}
	if got := len(source.calls()); got != 2 {
		t.Fatalf("provider calls after late target = %d, want 2", got)
	}
}

func TestDefaultClientTimeoutCoversProviderBudget(t *testing.T) {
	if margin := defaultClientTimeout - provider.DefaultTimeout; margin != 2*time.Second {
		t.Fatalf("client timeout margin = %v, want 2s", margin)
	}
	if margin := defaultConnectionTimeout - provider.DefaultTimeout; margin != 5*time.Second {
		t.Fatalf("connection timeout margin = %v, want 5s", margin)
	}
}

func TestActiveDetectsValidatedLiveBroker(t *testing.T) {
	useShortRuntimeDir(t)
	server, err := NewServer(sampleConfig(t), "production", &recordingSource{code: "246810"}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	active, err := Active("production")
	if err != nil || !active {
		t.Fatalf("active = %v, err = %v", active, err)
	}
}

func TestActiveReportsAbsentAndStaleBrokerAsInactive(t *testing.T) {
	useShortRuntimeDir(t)
	active, err := Active("production")
	if err != nil || active {
		t.Fatalf("absent active = %v, err = %v", active, err)
	}
	dir, err := runtimepath.SecureDir()
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "broker-production.sock")
	lease := filepath.Join(dir, "broker-production.lease")
	if err := runtimepath.WriteLease(lease, runtimepath.Lease{
		Kind:    "broker",
		Profile: "production",
		Socket:  socket,
		Identity: runtimepath.Identity{
			PID:        99999,
			UID:        uint32(os.Getuid()),
			Start:      "stale",
			Executable: "jumpotp",
		},
	}); err != nil {
		t.Fatal(err)
	}
	active, err = Active("production")
	if err != nil || active {
		t.Fatalf("stale active = %v, err = %v", active, err)
	}
}

func TestActiveRejectsConflictingBrokerState(t *testing.T) {
	useShortRuntimeDir(t)
	dir, err := runtimepath.SecureDir()
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "broker-production.sock")
	lease := filepath.Join(dir, "broker-production.lease")
	identity, err := runtimepath.CurrentIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimepath.WriteLease(lease, runtimepath.Lease{
		Kind:     "tmux",
		Profile:  "other",
		Socket:   socket,
		Identity: identity,
	}); err != nil {
		t.Fatal(err)
	}
	if active, err := Active("production"); err == nil || active {
		t.Fatalf("conflicting active = %v, err = %v", active, err)
	}
}

func TestActiveRejectsSocketWithoutValidatedLease(t *testing.T) {
	useShortRuntimeDir(t)
	dir, err := runtimepath.SecureDir()
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "broker-production.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if active, err := Active("production"); err == nil || active {
		t.Fatalf("unleased active = %v, err = %v", active, err)
	}
}

func TestBrokerSharesInFlightFetchWithLaterReadyTarget(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &blockingSource{started: make(chan struct{}), release: make(chan struct{})}
	server, err := NewServer(cfg, "production", source, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	disableRotationGuard(server)
	go server.Serve(context.Background())
	defer server.Close()

	results := make(chan error, 2)
	request := func(target, nonce string) {
		client := &Client{Socket: server.Socket(), Profile: "production", Target: target, Nonce: nonce, Timeout: time.Second}
		code, err := client.Code(context.Background(), "ignored")
		if err == nil && string(code) != "246810" {
			err = errors.New("unexpected code")
		}
		results <- err
	}
	go request("app-01", "first")
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("provider fetch did not start")
	}
	go request("app-01", "later-during-fetch")
	time.Sleep(50 * time.Millisecond)
	close(source.release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if got := source.callCount(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestTargetOverrideUsesSeparateGroup(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &recordingSource{code: "246810"}
	server, err := NewServer(cfg, "production", source, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	disableRotationGuard(server)
	go server.Serve(context.Background())
	defer server.Close()
	var wait sync.WaitGroup
	for index, target := range []string{"app-01", "app-02"} {
		wait.Add(1)
		go func(index int, target string) {
			defer wait.Done()
			client := &Client{Socket: server.Socket(), Profile: "production", Target: target, Nonce: string(rune('x' + index)), Timeout: time.Second}
			if _, err := client.Code(context.Background(), "ignored"); err != nil {
				t.Errorf("%s: %v", target, err)
			}
		}(index, target)
	}
	wait.Wait()
	calls := source.calls()
	if len(calls) != 2 || calls[0] == calls[1] {
		t.Fatalf("provider items = %#v", calls)
	}
}

func TestBrokerRejectsWrongTargetAndDuplicateNonce(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &recordingSource{code: "246810"}
	server, err := NewServer(cfg, "production", source, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	disableRotationGuard(server)
	go server.Serve(context.Background())
	defer server.Close()

	wrong := &Client{Socket: server.Socket(), Profile: "production", Target: "missing", Nonce: "wrong", Timeout: time.Second}
	if _, err := wrong.Code(context.Background(), "ignored"); err == nil {
		t.Fatal("wrong target accepted")
	}
	client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Nonce: "same", Timeout: time.Second}
	if _, err := client.Code(context.Background(), "ignored"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Code(context.Background(), "ignored"); err == nil {
		t.Fatal("duplicate nonce accepted")
	}
	if got := len(source.calls()); got != 1 {
		t.Fatalf("provider calls = %d", got)
	}
}

func TestSocketPermissionsAndOversizedFrame(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	server, err := NewServer(cfg, "production", &recordingSource{code: "246810"}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(context.Background())
	defer server.Close()
	info, err := os.Stat(server.Socket())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o", info.Mode().Perm())
	}
	connection, err := net.Dial("unix", server.Socket())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], maxFrameBytes+1)
	if _, err := connection.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	var result response
	if err := readFrame(connection, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "error" {
		t.Fatalf("response = %+v", result)
	}
}

func TestProviderFailureIsRedacted(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &recordingSource{err: errors.New("private provider detail")}
	server, err := NewServer(cfg, "production", source, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	disableRotationGuard(server)
	go server.Serve(context.Background())
	defer server.Close()
	client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Timeout: time.Second}
	_, err = client.Code(context.Background(), "ignored")
	if err == nil || err.Error() == "private provider detail" {
		t.Fatalf("error = %v", err)
	}
}

func TestBrokerPreservesMeasuredProviderTiming(t *testing.T) {
	useShortRuntimeDir(t)
	source := &recordingSource{err: provider.NewMeasuredError(provider.TimedOut, 7)}
	server, err := NewServer(sampleConfig(t), "production", source, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	disableRotationGuard(server)
	go server.Serve(context.Background())
	defer server.Close()
	client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Timeout: time.Second}
	_, err = client.Code(context.Background(), "ignored")
	if got := provider.SafeMessage(err); got != "Bitwarden TOTP retrieval timed out after 7s" {
		t.Fatalf("safe message = %q", got)
	}
}

func TestBrokerResponseTimingIsOptionalBoundedAndMessageIndependent(t *testing.T) {
	seven := 7
	negative := -1
	tooLarge := 21
	tests := []struct {
		name    string
		seconds *int
		want    string
	}{
		{name: "valid", seconds: &seven, want: "Bitwarden TOTP retrieval timed out after 7s"},
		{name: "absent", want: "Bitwarden TOTP retrieval timed out"},
		{name: "negative", seconds: &negative, want: "Bitwarden TOTP retrieval timed out"},
		{name: "above deadline", seconds: &tooLarge, want: "Bitwarden TOTP retrieval timed out"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := responseError(response{
				Version:        protocolVersion,
				Status:         "error",
				Kind:           string(provider.TimedOut),
				Message:        "private item and provider output",
				ElapsedSeconds: test.seconds,
			})
			if got := provider.SafeMessage(err); got != test.want {
				t.Fatalf("safe message = %q, want %q", got, test.want)
			}
		})
	}
}

func TestClientReconnectsWhenBrokerAppearsLater(t *testing.T) {
	useShortRuntimeDir(t)
	dir, err := runtimepath.SecureDir()
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{
		Socket:  filepath.Join(dir, "broker-production.sock"),
		Profile: "production",
		Target:  "app-01",
		Timeout: 50 * time.Millisecond,
	}
	result := make(chan error, 1)
	go func() {
		code, err := client.AwaitCode(context.Background(), "ignored")
		if err == nil && string(code) != "246810" {
			err = errors.New("unexpected code")
		}
		result <- err
	}()
	time.Sleep(100 * time.Millisecond)
	server, err := NewServer(sampleConfig(t), "production", &recordingSource{code: "246810"}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	disableRotationGuard(server)
	go server.Serve(context.Background())
	defer server.Close()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not reconnect")
	}
}

// guardProbe is both a provider.Source and a target for the sleep test seam,
// so a single mutex guards every observation a guard test makes: the call
// order between "sleep" and "provider", and the delay the sleep seam saw.
// This mirrors recordingSource's pattern of only ever reading captured state
// back out through the same lock that guards the write.
type guardProbe struct {
	mu    sync.Mutex
	log   []string
	delay time.Duration
	code  string
}

func (p *guardProbe) recordSleep(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.delay = d
	p.log = append(p.log, "sleep")
}

func (p *guardProbe) Code(_ context.Context, _ string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, "provider")
	return []byte(p.code), nil
}

func (p *guardProbe) snapshot() (log []string, delay time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.log...), p.delay
}

// epochSecond builds a wall-clock time whose position within the 30-second
// TOTP window is exactly second, so tests can pick deterministic remaining
// runway without depending on the real clock.
func epochSecond(second int) time.Time {
	return time.Unix(int64(second), 0)
}

// disableRotationGuard pins server's now seam to the start of a TOTP window,
// so flush never observes a real, wall-clock-dependent rotation-boundary
// delay. Pre-existing tests that don't exercise the guard use this so their
// short client timeouts aren't at the mercy of when in a real 30-second
// window the test happens to run.
func disableRotationGuard(server *Server) {
	server.now = func() time.Time { return epochSecond(0) }
}

func TestFlushDelaysRetrievalWhenLessThanGuardThresholdRemains(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &guardProbe{code: "246810"}
	server, err := NewServer(cfg, "production", source, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Second 27 of the window leaves 3s of runway, under the 8s guard
	// threshold, so flush must wait 3s + the 300ms skew allowance.
	server.now = func() time.Time { return epochSecond(27) }
	wantDelay := 3*time.Second + rotationGuardSkew
	server.sleep = func(ctx context.Context, d time.Duration) bool {
		source.recordSleep(d)
		return true
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	defer func() {
		_ = server.Close()
		<-serveDone
	}()

	client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Nonce: "guard-delay", Timeout: time.Second}
	code, err := client.Code(context.Background(), "ignored")
	if err != nil || string(code) != "246810" {
		t.Fatalf("code = %q, err = %v", code, err)
	}
	log, gotDelay := source.snapshot()
	if gotDelay != wantDelay {
		t.Fatalf("guard delay = %v, want %v", gotDelay, wantDelay)
	}
	if len(log) != 2 || log[0] != "sleep" || log[1] != "provider" {
		t.Fatalf("call order = %#v, want [sleep provider]", log)
	}
}

func TestFlushSkipsDelayWithSufficientRunway(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &guardProbe{code: "246810"}
	server, err := NewServer(cfg, "production", source, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Second 10 of the window leaves 20s of runway, at or above the 8s
	// guard threshold, so flush must fetch immediately without waiting.
	server.now = func() time.Time { return epochSecond(10) }
	sleepCalled := false
	server.sleep = func(ctx context.Context, d time.Duration) bool {
		sleepCalled = true
		return true
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	defer func() {
		_ = server.Close()
		<-serveDone
	}()

	client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Nonce: "no-guard-delay", Timeout: time.Second}
	code, err := client.Code(context.Background(), "ignored")
	if err != nil || string(code) != "246810" {
		t.Fatalf("code = %q, err = %v", code, err)
	}
	if sleepCalled {
		t.Fatal("sleep seam was invoked despite sufficient runway")
	}
	log, _ := source.snapshot()
	if len(log) != 1 || log[0] != "provider" {
		t.Fatalf("call order = %#v, want [provider]", log)
	}
}

func TestFlushGuardDelayDeliversCodeToAllAggregatedWaiters(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &guardProbe{code: "246810"}
	server, err := NewServer(cfg, "production", source, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	server.now = func() time.Time { return epochSecond(25) }
	server.sleep = func(ctx context.Context, d time.Duration) bool {
		source.recordSleep(d)
		return true
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	defer func() {
		_ = server.Close()
		<-serveDone
	}()

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 2)
	for _, nonce := range []string{"a", "b"} {
		go func(nonce string) {
			client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Nonce: nonce, Timeout: time.Second}
			code, err := client.Code(context.Background(), "ignored")
			results <- result{code: string(code), err: err}
		}(nonce)
	}
	for range 2 {
		got := <-results
		if got.err != nil || got.code != "246810" {
			t.Fatalf("result = %+v", got)
		}
	}
	log, _ := source.snapshot()
	providerCalls := 0
	for _, entry := range log {
		if entry == "provider" {
			providerCalls++
		}
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %#v, want exactly one call for the whole batch", log)
	}
}

func TestFlushGuardWaitUnblocksPromptlyOnShutdown(t *testing.T) {
	useShortRuntimeDir(t)
	cfg := sampleConfig(t)
	source := &recordingSource{code: "246810"}
	server, err := NewServer(cfg, "production", source, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	// Second 29 leaves 1s of runway, so the guard schedules a real
	// (production) wait of 1s + 300ms skew; Close must cut that short.
	server.now = func() time.Time { return epochSecond(29) }
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()

	result := make(chan error, 1)
	go func() {
		client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Nonce: "shutdown-during-guard", Timeout: 2 * time.Second}
		_, err := client.Code(context.Background(), "ignored")
		result <- err
	}()
	// Give the aggregation window time to fire and enter the guard wait
	// before shutting the broker down.
	time.Sleep(30 * time.Millisecond)
	closeStart := time.Now()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	<-serveDone

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("expected an interruption error, got nil")
		}
		if kind := provider.KindOf(err); kind != provider.Interrupted {
			t.Fatalf("error kind = %v, want %v", kind, provider.Interrupted)
		}
		if elapsed := time.Since(closeStart); elapsed > 500*time.Millisecond {
			t.Fatalf("shutdown took %v to unblock the guard wait, want well under the 1.3s guard delay", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not unblock the guard wait")
	}
	if len(source.calls()) != 0 {
		t.Fatalf("provider was called despite shutdown during the guard wait: %#v", source.calls())
	}
}

func TestRotationGuardDelay(t *testing.T) {
	tests := []struct {
		name   string
		second int
		want   time.Duration
	}{
		{name: "start of window", second: 0, want: 0},
		{name: "just under threshold boundary", second: 21, want: 0},
		{name: "exactly at threshold", second: 22, want: 0},
		{name: "one second inside threshold", second: 23, want: 7*time.Second + rotationGuardSkew},
		{name: "one second before boundary", second: 29, want: 1*time.Second + rotationGuardSkew},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := rotationGuardDelay(epochSecond(test.second))
			if got != test.want {
				t.Fatalf("rotationGuardDelay(second %d) = %v, want %v", test.second, got, test.want)
			}
		})
	}
}

func sampleConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(config.Sample))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func useShortRuntimeDir(t *testing.T) {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "jotp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	t.Setenv("XDG_RUNTIME_DIR", base)
}
