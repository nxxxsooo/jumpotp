package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
	runtimepath "github.com/nxxxsooo/jumpotp/internal/runtime"
)

func TestIsolatedWorkspaceLifecycle(t *testing.T) {
	requireTmux(t)
	useWorkspaceRuntime(t)
	cfg, wrapper := workspaceFixture(t)
	before := defaultTmuxStatus()
	manager := Manager{Config: cfg, Executable: wrapper}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := manager.Ensure(ctx, "production", "", false, filepath.Join(t.TempDir(), "broker.sock")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = manager.Stop(context.Background(), "production") })
	if err := manager.Ensure(ctx, "production", "", false, filepath.Join(t.TempDir(), "broker.sock")); err != nil {
		t.Fatalf("duplicate Ensure: %v", err)
	}
	report, err := manager.Status(ctx, "production")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Workspaces) != 1 || len(report.Workspaces[0].Targets) != 2 {
		t.Fatalf("report = %+v", report)
	}
	stopped, err := manager.Stop(ctx, "production")
	if err != nil || !stopped {
		t.Fatalf("Stop = %v, %v", stopped, err)
	}
	report, err = manager.Status(ctx, "production")
	if err != nil {
		t.Fatalf("Status after final Stop: %v", err)
	}
	if len(report.Workspaces) != 0 {
		t.Fatalf("Status after final Stop = %+v", report)
	}
	after := defaultTmuxStatus()
	if before != after {
		t.Fatalf("default tmux state changed\nbefore: %q\nafter: %q", before, after)
	}
}

func TestConflictingLeaseFailsClosed(t *testing.T) {
	requireTmux(t)
	useWorkspaceRuntime(t)
	cfg, wrapper := workspaceFixture(t)
	manager := Manager{Config: cfg, Executable: wrapper}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := manager.Ensure(ctx, "production", "", false, filepath.Join(t.TempDir(), "broker.sock")); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background(), "production")
	socket, leasePath, err := manager.Paths()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := runtimepath.ReadLease(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	lease.Identity.Start = "synthetic-wrong-start"
	if err := runtimepath.WriteLease(leasePath, lease); err != nil {
		t.Fatal(err)
	}
	if err := manager.Ensure(ctx, "production", "", false, filepath.Join(t.TempDir(), "broker.sock")); err == nil {
		t.Fatal("conflicting lease accepted")
	}
	output, err := exec.Command("tmux", "-S", socket, "has-session", "-t", "production").CombinedOutput()
	if err != nil {
		t.Fatalf("healthy isolated tmux was killed: %v: %s", err, output)
	}
}

func TestValidatedUnresponsiveServerRecovery(t *testing.T) {
	requireTmux(t)
	useWorkspaceRuntime(t)
	cfg, wrapper := workspaceFixture(t)
	manager := Manager{Config: cfg, Executable: wrapper}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	brokerSocket := filepath.Join(t.TempDir(), "broker.sock")
	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background(), "production")
	_, leasePath, err := manager.Paths()
	if err != nil {
		t.Fatal(err)
	}
	oldLease, err := runtimepath.ReadLease(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	process, err := os.FindProcess(oldLease.Identity.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	socket, _, err := manager.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.recoverServer(socket, leasePath); err != nil {
		_ = process.Signal(syscall.SIGKILL)
		t.Fatalf("recoverServer: %v", err)
	}
	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err != nil {
		t.Fatalf("recover Ensure: %v", err)
	}
	newLease, err := runtimepath.ReadLease(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	if newLease.Identity.PID == oldLease.Identity.PID {
		t.Fatalf("server PID did not change: %d", newLease.Identity.PID)
	}
}

func TestUnexpectedTargetPaneIsPreserved(t *testing.T) {
	requireTmux(t)
	useWorkspaceRuntime(t)
	cfg, wrapper := workspaceFixture(t)
	manager := Manager{Config: cfg, Executable: wrapper}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	brokerSocket := filepath.Join(t.TempDir(), "broker.sock")
	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background(), "production")
	socket, _, _ := manager.Paths()
	if output, err := exec.Command("tmux", "-S", socket, "set-option", "-w", "-t", "production:app-01", "@jumpotp_target", "other").CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v: %s", err, output)
	}
	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("Ensure error = %v", err)
	}
}

// TestSupervisedWrapperOutlivesChildExitAndReconcileStaysHealthy exercises
// the pane-level contract that internal/supervise.Supervisor's reconnect
// loop depends on: reconcileSession decides pane health from tmux's own
// pane_dead/marker state, not from whether the wrapper's inner launcher
// child is still running. It simulates the supervisor (see design.md, "D2:
// Supervised reconnection in the target wrapper") with a plain shell wrapper
// that runs a short-lived "child" and then keeps the pane's process alive --
// exactly as Supervisor.Run does while backing off or waiting on the
// reconnect gate -- rather than driving the real supervisor binary and ssh
// inside tmux, which is impractical here; internal/supervise/supervise_test.go
// covers the actual backoff/gate/signal state machine at the unit level.
func TestSupervisedWrapperOutlivesChildExitAndReconcileStaysHealthy(t *testing.T) {
	requireTmux(t)
	useWorkspaceRuntime(t)
	cfg, wrapper := supervisedWrapperFixture(t)
	manager := Manager{Config: cfg, Executable: wrapper}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	brokerSocket := filepath.Join(t.TempDir(), "broker.sock")
	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = manager.Stop(context.Background(), "production") })
	socket, _, err := manager.Paths()
	if err != nil {
		t.Fatal(err)
	}
	beforePaneID := paneID(t, socket, "production:app-01")

	// Give the simulated child time to exit inside the wrapper while the
	// wrapper's own process (the pane's process, from tmux's perspective)
	// keeps running -- the property a live supervised reconnect depends on.
	time.Sleep(2 * time.Second)

	if dead := paneField(t, socket, "production:app-01", "#{pane_dead}"); dead != "0" {
		t.Fatalf("pane_dead = %q, want the pane alive after the simulated child exited", dead)
	}

	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err != nil {
		t.Fatalf("reconcileSession over a live supervised pane: %v", err)
	}

	if afterPaneID := paneID(t, socket, "production:app-01"); afterPaneID != beforePaneID {
		t.Fatalf("pane id changed from %q to %q; reconcileSession recreated a live supervised window instead of leaving it alone", beforePaneID, afterPaneID)
	}
}

// TestReconcileSessionRejectsGenuinelyDeadPane covers the fail-closed branch
// of reconcileSession that TestUnexpectedTargetPaneIsPreserved does not: a
// pane whose process has actually exited (pane_dead=1), as opposed to a live
// pane carrying an unexpected @jumpotp_target marker. tmux only keeps a dead
// pane's window around (rather than closing it immediately) when
// remain-on-exit is enabled, so the test sets that window option before
// killing the wrapper process to put reconcileSession's dead-pane check on a
// real code path.
func TestReconcileSessionRejectsGenuinelyDeadPane(t *testing.T) {
	requireTmux(t)
	useWorkspaceRuntime(t)
	cfg, wrapper := workspaceFixture(t)
	manager := Manager{Config: cfg, Executable: wrapper}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	brokerSocket := filepath.Join(t.TempDir(), "broker.sock")
	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background(), "production")
	socket, _, err := manager.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("tmux", "-S", socket, "set-option", "-w", "-t", "production:app-01", "remain-on-exit", "on").CombinedOutput(); err != nil {
		t.Fatalf("set remain-on-exit: %v: %s", err, output)
	}
	panePID, err := strconv.Atoi(paneField(t, socket, "production:app-01", "#{pane_pid}"))
	if err != nil {
		t.Fatalf("parse pane pid: %v", err)
	}
	if err := syscall.Kill(panePID, syscall.SIGKILL); err != nil {
		t.Fatalf("kill wrapper pane process: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for paneField(t, socket, "production:app-01", "#{pane_dead}") != "1" {
		if time.Now().After(deadline) {
			t.Fatal("pane never reported dead after the wrapper process was killed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := manager.Ensure(ctx, "production", "", false, brokerSocket); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("Ensure error = %v, want a fail-closed refusal for a genuinely dead pane", err)
	}
}

func requireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is unavailable")
	}
}

func useWorkspaceRuntime(t *testing.T) {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "jotp-ws-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	t.Setenv("XDG_RUNTIME_DIR", base)
}

func workspaceFixture(t *testing.T) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config.Sample), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "wrapper")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, wrapper
}

// supervisedWrapperFixture builds a workspace whose target command simulates
// a supervised __target wrapper: a short-lived "child" (the first sleep,
// standing in for a launcher connection that ends on its own) exits on its
// own, and the wrapper keeps the pane's process alive afterward exactly as
// internal/supervise.Supervisor does while backing off or waiting on the
// reconnect gate, instead of letting the pane die with it.
func supervisedWrapperFixture(t *testing.T) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config.Sample), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(dir, "wrapper")
	script := "#!/bin/sh\nsleep 1\nexec sleep 30\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, wrapper
}

func paneID(t *testing.T, socket, target string) string {
	t.Helper()
	return paneField(t, socket, target, "#{pane_id}")
}

func paneField(t *testing.T, socket, target, format string) string {
	t.Helper()
	output, err := exec.Command("tmux", "-S", socket, "display-message", "-p", "-t", target, format).CombinedOutput()
	if err != nil {
		t.Fatalf("read %s for %s: %v: %s", format, target, err, output)
	}
	return strings.TrimSpace(string(output))
}

func defaultTmuxStatus() string {
	output, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}\t#{session_attached}").CombinedOutput()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return "exit:" + strings.TrimSpace(string(output))
		}
		return "error"
	}
	return string(output)
}
