package workspace

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

type statusRunner struct {
	windows    []byte
	masterFail bool
}

func (r *statusRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	switch {
	case name == "tmux" && containsArg(args, "list-sessions") && !containsArg(args, "-F"):
		return nil, nil
	case name == "tmux" && containsArg(args, "list-sessions"):
		return []byte("production\t0\n"), nil
	case name == "tmux" && containsArg(args, "list-windows"):
		return r.windows, nil
	case name == "ssh" && containsArg(args, "check"):
		if r.masterFail {
			return nil, errors.New("no master running")
		}
		return []byte("Master running (pid 1)\n"), nil
	}
	return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
}

func (r *statusRunner) Interactive(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error {
	return errors.New("unexpected interactive call")
}

func (r *statusRunner) LookPath(name string) (string, error) {
	return "/synthetic/bin/" + name, nil
}

func statusManager(t *testing.T, runner *statusRunner) Manager {
	t.Helper()
	useWorkspaceRuntime(t)
	cfg, err := config.Parse([]byte(config.Sample))
	if err != nil {
		t.Fatal(err)
	}
	manager := Manager{Config: cfg, Runner: runner}
	socket, _, err := manager.Paths()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return manager
}

func targetState(t *testing.T, report StatusReport, target string) TargetStatus {
	t.Helper()
	if len(report.Workspaces) != 1 {
		t.Fatalf("workspaces = %+v", report.Workspaces)
	}
	for _, current := range report.Workspaces[0].Targets {
		if current.Target == target {
			return current
		}
	}
	t.Fatalf("target %q not found in %+v", target, report.Workspaces[0].Targets)
	return TargetStatus{}
}

func TestStatusReportsConnectingWhenWrapperAliveButMasterUnavailable(t *testing.T) {
	runner := &statusRunner{
		windows:    []byte("app-01\t0\tapp-01\t0\napp-02\t0\tapp-02\t0\n"),
		masterFail: true,
	}
	manager := statusManager(t, runner)
	report, err := manager.Status(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	target := targetState(t, report, "app-01")
	if target.State != "connecting" || target.ControlMaster != "unavailable" {
		t.Fatalf("target = %+v", target)
	}
}

func TestStatusReportsRunningWhenWrapperAliveAndMasterAvailable(t *testing.T) {
	runner := &statusRunner{
		windows:    []byte("app-01\t0\tapp-01\t0\napp-02\t0\tapp-02\t0\n"),
		masterFail: false,
	}
	manager := statusManager(t, runner)
	report, err := manager.Status(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	target := targetState(t, report, "app-01")
	if target.State != "running" || target.ControlMaster != "available" {
		t.Fatalf("target = %+v", target)
	}
}

func TestStatusReportsStoppedWhenWindowAbsent(t *testing.T) {
	runner := &statusRunner{
		windows:    []byte(""),
		masterFail: true,
	}
	manager := statusManager(t, runner)
	report, err := manager.Status(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	target := targetState(t, report, "app-01")
	if target.State != "stopped" {
		t.Fatalf("target = %+v", target)
	}
}

func TestStatusReportsFailedRegardlessOfMasterAvailability(t *testing.T) {
	runner := &statusRunner{
		windows:    []byte("app-01\t1\tapp-01\t0\napp-02\t0\tother\t0\n"),
		masterFail: false,
	}
	manager := statusManager(t, runner)
	report, err := manager.Status(context.Background(), "production")
	if err != nil {
		t.Fatal(err)
	}
	deadPane := targetState(t, report, "app-01")
	if deadPane.State != "failed" {
		t.Fatalf("dead pane target = %+v", deadPane)
	}
	mismatchedMarker := targetState(t, report, "app-02")
	if mismatchedMarker.State != "failed" {
		t.Fatalf("mismatched marker target = %+v", mismatchedMarker)
	}
}
