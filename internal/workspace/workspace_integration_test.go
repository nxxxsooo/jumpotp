package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
