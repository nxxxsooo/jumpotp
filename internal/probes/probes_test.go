package probes

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

type fakeRunner struct {
	mu          sync.Mutex
	calls       [][]string
	masterAlive bool
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if len(args) >= 2 && args[0] == "-O" && args[1] == "check" && !f.masterAlive {
		return nil, errors.New("no master")
	}
	return []byte("synthetic probe output\n"), nil
}

func (f *fakeRunner) snapshot() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.calls...)
}

func TestDisabledHealthRunsNothing(t *testing.T) {
	cfg, err := config.Parse([]byte(config.Sample))
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{masterAlive: true}
	var output strings.Builder
	if err := (Scheduler{Config: cfg, Profile: "production", Runner: runner, Out: &output}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(runner.snapshot()) != 0 || !strings.Contains(output.String(), "disabled") {
		t.Fatalf("calls = %#v, output = %q", runner.snapshot(), output.String())
	}
}

func TestMissingMasterSkipsProbe(t *testing.T) {
	cfg := enabledConfig(t)
	runner := &fakeRunner{masterAlive: false}
	ctx, cancel := context.WithCancel(context.Background())
	var output strings.Builder
	done := make(chan error, 1)
	go func() { done <- (Scheduler{Config: cfg, Profile: "production", Runner: runner, Out: &output}).Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	calls := runner.snapshot()
	if len(calls) != 1 || strings.Join(calls[0], " ") != "ssh -O check production-app-01" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestProbeUsesDiscreteBatchArguments(t *testing.T) {
	cfg := enabledConfig(t)
	runner := &fakeRunner{masterAlive: true}
	ctx, cancel := context.WithCancel(context.Background())
	var output strings.Builder
	done := make(chan error, 1)
	go func() { done <- (Scheduler{Config: cfg, Profile: "production", Runner: runner, Out: &output}).Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for len(runner.snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	calls := runner.snapshot()
	if len(calls) < 2 {
		t.Fatalf("calls = %#v", calls)
	}
	wantPrefix := "ssh -o BatchMode=yes -o ConnectTimeout=10 production-app-01 "
	if got := strings.Join(calls[1], " "); !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("probe call = %q", got)
	}
	for _, call := range calls {
		if call[0] == "bw" {
			t.Fatalf("probe contacted provider: %#v", call)
		}
	}
}

func enabledConfig(t *testing.T) *config.Config {
	t.Helper()
	sample := strings.Replace(config.Sample, "enabled: false\n        interval: 4m", "enabled: true\n        interval: 30s", 1)
	cfg, err := config.Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
