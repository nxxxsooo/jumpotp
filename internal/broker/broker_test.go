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
	if defaultClientTimeout <= provider.DefaultTimeout {
		t.Fatalf("client timeout %v must exceed provider timeout %v", defaultClientTimeout, provider.DefaultTimeout)
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
	go server.Serve(context.Background())
	defer server.Close()
	client := &Client{Socket: server.Socket(), Profile: "production", Target: "app-01", Timeout: time.Second}
	_, err = client.Code(context.Background(), "ignored")
	if err == nil || err.Error() == "private provider detail" {
		t.Fatalf("error = %v", err)
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
