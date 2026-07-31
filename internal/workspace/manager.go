package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
	runtimepath "github.com/nxxxsooo/jumpotp/internal/runtime"
)

const tmuxTimeout = 5 * time.Second

type Manager struct {
	Config     *config.Config
	Executable string
	Runner     Runner
	Notice     io.Writer
}

type AttachStreams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	TTY string
}

type serverState int

const (
	serverAbsent serverState = iota
	serverHealthy
	serverUnhealthy
)

func (m Manager) Paths() (socket, lease string, err error) {
	dir, err := runtimepath.SecureDir()
	if err != nil {
		return "", "", err
	}
	socket = filepath.Join(dir, "tmux.sock")
	lease = filepath.Join(dir, "tmux.lease")
	if len(socket) > 100 {
		return "", "", errors.New("tmux socket path exceeds the platform safety limit")
	}
	return socket, lease, nil
}

func (m Manager) Ensure(ctx context.Context, profile, launcherOverride string, manual bool, brokerSocket string) error {
	if m.Config == nil {
		return errors.New("workspace configuration is required")
	}
	profileConfig, ok := m.Config.Profiles[profile]
	if !ok {
		return fmt.Errorf("profile %q is not configured", profile)
	}
	socket, lease, err := m.Paths()
	if err != nil {
		return err
	}
	state, stateErr := m.checkServer(ctx, socket)
	if state == serverUnhealthy {
		if m.Notice != nil {
			fmt.Fprintln(m.Notice, "JumpOTP tmux recovery resets all JumpOTP workspaces; rebuilding the requested profile.")
		}
		if recoverErr := m.recoverServer(socket, lease); recoverErr != nil {
			return fmt.Errorf("JumpOTP tmux server is unhealthy: %w", recoverErr)
		}
		state = serverAbsent
	}
	if stateErr != nil && state != serverAbsent {
		return stateErr
	}
	if state == serverAbsent {
		createErr := m.createSession(ctx, socket, profile, profileConfig, launcherOverride, manual, brokerSocket)
		leaseErr := m.writeServerLease(ctx, socket, lease)
		if createErr != nil {
			if leaseErr != nil {
				return fmt.Errorf("%v; additionally failed to record the new tmux server identity: %w", createErr, leaseErr)
			}
			return createErr
		}
		return leaseErr
	}
	if err := m.ensureServerLease(ctx, socket, lease); err != nil {
		return err
	}
	exists, err := m.hasSession(ctx, socket, profile)
	if err != nil {
		return err
	}
	if !exists {
		return m.createSession(ctx, socket, profile, profileConfig, launcherOverride, manual, brokerSocket)
	}
	return m.reconcileSession(ctx, socket, profile, profileConfig, launcherOverride, manual, brokerSocket)
}

func (m Manager) Attach(ctx context.Context, profile string, streams AttachStreams) error {
	socket, _, err := m.Paths()
	if err != nil {
		return err
	}
	runner := m.runner()
	tmuxEnv := os.Getenv("TMUX")
	if tmuxEnv == "" {
		return runner.Interactive(ctx, "tmux", []string{"-S", socket, "attach-session", "-t", profile}, streams.In, streams.Out, streams.Err)
	}
	currentSocket := strings.SplitN(tmuxEnv, ",", 2)[0]
	currentSocket, _ = filepath.Abs(currentSocket)
	expectedSocket, _ := filepath.Abs(socket)
	if currentSocket != expectedSocket {
		return errors.New("refusing nested tmux attachment; detach from the current tmux client first")
	}
	if _, err := m.command(ctx, socket, "switch-client", "-t", profile); err != nil {
		return err
	}
	if streams.TTY == "" {
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			output, err := m.command(ctx, socket, "list-clients", "-F", "#{client_tty}\t#{session_name}")
			if err != nil {
				return nil
			}
			if !clientOnProfile(string(output), streams.TTY, profile) {
				return nil
			}
		}
	}
}

func (m Manager) Stop(ctx context.Context, profile string) (bool, error) {
	socket, lease, err := m.Paths()
	if err != nil {
		return false, err
	}
	state, _ := m.checkServer(ctx, socket)
	if state == serverAbsent {
		return false, nil
	}
	if state == serverUnhealthy {
		return false, errors.New("JumpOTP tmux server is unhealthy; stop refused without validated recovery")
	}
	exists, err := m.hasSession(ctx, socket, profile)
	if err != nil || !exists {
		return false, err
	}
	sessions, err := m.command(ctx, socket, "list-sessions", "-F", "#{session_name}")
	if err != nil {
		return false, err
	}
	lastSession := len(strings.Fields(string(sessions))) == 1
	if _, err := m.command(ctx, socket, "kill-session", "-t", profile); err != nil {
		return false, err
	}
	if lastSession {
		if err := m.recoverServer(socket, lease); err != nil {
			return true, fmt.Errorf("clean up final JumpOTP tmux server: %w", err)
		}
	}
	return true, nil
}

func (m Manager) createSession(ctx context.Context, socket, profile string, profileConfig config.Profile, launcherOverride string, manual bool, brokerSocket string) error {
	targets := sortedTargets(profileConfig.Targets)
	if len(targets) == 0 {
		return errors.New("profile has no targets")
	}
	executable, err := m.executable()
	if err != nil {
		return err
	}
	first := targets[0]
	args := []string{"new-session", "-d", "-s", profile, "-n", first, "-e", "JUMPOTP_INTERNAL=1", "--"}
	args = append(args, m.targetCommand(executable, profile, first, launcherOverride, manual, brokerSocket)...)
	if _, err := m.command(ctx, socket, args...); err != nil {
		return fmt.Errorf("create workspace session: %w", err)
	}
	if _, err := m.command(ctx, socket, "set-option", "-t", profile, "@jumpotp_profile", profile); err != nil {
		return err
	}
	if err := m.markTarget(ctx, socket, profile, first); err != nil {
		return err
	}
	for _, target := range targets[1:] {
		if err := m.createTargetWindow(ctx, socket, profile, target, executable, launcherOverride, manual, brokerSocket); err != nil {
			return err
		}
	}
	if profileConfig.Workspace != nil && profileConfig.Workspace.Health != nil && profileConfig.Workspace.Health.Enabled {
		if err := m.createHealthWindow(ctx, socket, profile, executable); err != nil {
			return err
		}
	}
	return nil
}

func (m Manager) reconcileSession(ctx context.Context, socket, profile string, profileConfig config.Profile, launcherOverride string, manual bool, brokerSocket string) error {
	output, err := m.command(ctx, socket, "list-windows", "-t", profile, "-F", "#{window_name}\t#{pane_id}\t#{pane_dead}\t#{@jumpotp_target}\t#{@jumpotp_health}")
	if err != nil {
		return err
	}
	type window struct {
		paneID string
		dead   string
		target string
		health string
	}
	windows := map[string]window{}
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 5 || parts[0] == "" || parts[1] == "" {
			return errors.New("tmux returned an invalid window or pane identifier")
		}
		windows[parts[0]] = window{paneID: parts[1], dead: parts[2], target: parts[3], health: parts[4]}
	}
	executable, err := m.executable()
	if err != nil {
		return err
	}
	expected := map[string]bool{}
	for _, target := range sortedTargets(profileConfig.Targets) {
		expected[target] = true
		existing, ok := windows[target]
		if !ok {
			if err := m.createTargetWindow(ctx, socket, profile, target, executable, launcherOverride, manual, brokerSocket); err != nil {
				return err
			}
			continue
		}
		if existing.dead == "1" || existing.target != target {
			return fmt.Errorf("refusing to replace unexpected pane %s in %s:%s", existing.paneID, profile, target)
		}
	}
	healthExpected := profileConfig.Workspace != nil && profileConfig.Workspace.Health != nil && profileConfig.Workspace.Health.Enabled
	if healthExpected {
		expected["health"] = true
		if existing, ok := windows["health"]; !ok {
			if err := m.createHealthWindow(ctx, socket, profile, executable); err != nil {
				return err
			}
		} else if existing.dead == "1" || existing.health != "1" {
			return errors.New("refusing to replace unexpected health pane")
		}
	}
	for name := range windows {
		if !expected[name] {
			return fmt.Errorf("workspace contains unexpected window %q", name)
		}
	}
	return nil
}

func (m Manager) createTargetWindow(ctx context.Context, socket, profile, target, executable, launcherOverride string, manual bool, brokerSocket string) error {
	args := []string{"new-window", "-d", "-t", profile, "-n", target, "-e", "JUMPOTP_INTERNAL=1", "--"}
	args = append(args, m.targetCommand(executable, profile, target, launcherOverride, manual, brokerSocket)...)
	if _, err := m.command(ctx, socket, args...); err != nil {
		return fmt.Errorf("create target window %s: %w", target, err)
	}
	return m.markTarget(ctx, socket, profile, target)
}

func (m Manager) markTarget(ctx context.Context, socket, profile, target string) error {
	output, err := m.command(ctx, socket, "display-message", "-p", "-t", profile+":"+target, "#{pane_id}")
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return fmt.Errorf("identify target pane %s: %w", target, err)
	}
	if _, err := m.command(ctx, socket, "set-option", "-w", "-t", profile+":"+target, "@jumpotp_target", target); err != nil {
		return fmt.Errorf("mark target window %s: %w", target, err)
	}
	return nil
}

func (m Manager) createHealthWindow(ctx context.Context, socket, profile, executable string) error {
	args := []string{
		"new-window", "-d", "-t", profile, "-n", "health", "-e", "JUMPOTP_INTERNAL=1", "--",
		executable, "__health", "--config", m.Config.Path, "--profile", profile,
	}
	if _, err := m.command(ctx, socket, args...); err != nil {
		return fmt.Errorf("create health window: %w", err)
	}
	output, err := m.command(ctx, socket, "display-message", "-p", "-t", profile+":health", "#{pane_id}")
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return errors.New("identify health pane failed")
	}
	_, err = m.command(ctx, socket, "set-option", "-w", "-t", profile+":health", "@jumpotp_health", "1")
	return err
}

func (m Manager) targetCommand(executable, profile, target, launcherOverride string, manual bool, brokerSocket string) []string {
	args := []string{executable, "__target", "--config", m.Config.Path, "--profile", profile, "--target", target, "--broker", brokerSocket}
	if launcherOverride != "" {
		args = append(args, "--launcher", launcherOverride)
	}
	if manual {
		args = append(args, "--manual")
	}
	return args
}

func (m Manager) checkServer(ctx context.Context, socket string) (serverState, error) {
	info, err := os.Lstat(socket)
	if errors.Is(err, os.ErrNotExist) {
		return serverAbsent, nil
	}
	if err != nil {
		return serverUnhealthy, err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return serverUnhealthy, errors.New("tmux socket path is not a Unix socket")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return serverUnhealthy, errors.New("tmux socket is not owned by the current user")
	}
	var lastErr error
	for range 3 {
		if _, err := m.command(ctx, socket, "list-sessions"); err == nil {
			return serverHealthy, nil
		} else {
			lastErr = err
		}
	}
	return serverUnhealthy, lastErr
}

func (m Manager) hasSession(ctx context.Context, socket, profile string) (bool, error) {
	_, err := m.command(ctx, socket, "has-session", "-t", profile)
	if err == nil {
		return true, nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "can't find session") {
		return false, nil
	}
	return false, err
}

func (m Manager) ensureServerLease(ctx context.Context, socket, lease string) error {
	existing, err := runtimepath.ReadLease(lease)
	if err == nil {
		if existing.Kind != "tmux" || existing.Socket != socket || !runtimepath.Matches(existing.Identity) {
			return errors.New("tmux runtime lease conflicts with the live server")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("validate tmux lease: %w", err)
	}
	return m.writeServerLease(ctx, socket, lease)
}

func (m Manager) writeServerLease(ctx context.Context, socket, lease string) error {
	output, err := m.command(ctx, socket, "display-message", "-p", "#{pid}")
	if err != nil {
		return fmt.Errorf("query tmux server PID: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || pid <= 0 {
		return errors.New("tmux returned an invalid server PID")
	}
	identity, err := runtimepath.ProcessIdentity(pid)
	if err != nil {
		return fmt.Errorf("read tmux process identity: %w", err)
	}
	tmuxPath, err := m.runner().LookPath("tmux")
	if err != nil {
		return err
	}
	tmuxPath, err = filepath.Abs(tmuxPath)
	if err != nil {
		return err
	}
	if filepath.Base(identity.Executable) != filepath.Base(tmuxPath) {
		return fmt.Errorf("tmux PID executable identity does not match: process %q, executable %q", identity.Executable, filepath.Base(tmuxPath))
	}
	identity.Executable = tmuxPath
	return runtimepath.WriteLease(lease, runtimepath.Lease{Kind: "tmux", Socket: socket, Identity: identity})
}

func (m Manager) recoverServer(socket, lease string) error {
	value, err := runtimepath.ReadLease(lease)
	if err != nil {
		return fmt.Errorf("validated lease is required for recovery: %w", err)
	}
	if value.Kind != "tmux" || value.Socket != socket {
		return errors.New("socket, lease, PID, start identity, owner, or executable evidence conflicts")
	}
	current, identityErr := runtimepath.ProcessIdentity(value.Identity.PID)
	if identityErr != nil {
		if errors.Is(identityErr, os.ErrNotExist) || errors.Is(identityErr, syscall.ESRCH) {
			return quarantineRuntime(socket, lease)
		}
		return fmt.Errorf("cannot verify leased tmux process identity: %w", identityErr)
	}
	if current.UID != value.Identity.UID || current.Start != value.Identity.Start ||
		filepath.Base(current.Executable) != filepath.Base(value.Identity.Executable) {
		return errors.New("socket, lease, PID, start identity, owner, or executable evidence conflicts")
	}
	process, err := os.FindProcess(value.Identity.PID)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("gracefully stop tmux: %w", err)
	}
	deadline := time.Now().Add(750 * time.Millisecond)
	for runtimepath.Matches(value.Identity) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if runtimepath.Matches(value.Identity) {
		if err := process.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("force stop tmux: %w", err)
		}
	}
	return quarantineRuntime(socket, lease)
}

func quarantineRuntime(socket, lease string) error {
	suffix := ".stale-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := os.Lstat(socket); err == nil {
		if err := os.Rename(socket, socket+suffix); err != nil {
			return err
		}
	}
	if err := os.Rename(lease, lease+suffix); err != nil {
		return err
	}
	return nil
}

func (m Manager) command(ctx context.Context, socket string, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, tmuxTimeout)
	defer cancel()
	fullArgs := append([]string{"-S", socket}, args...)
	output, err := m.runner().Output(runCtx, "tmux", fullArgs...)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return output, errors.New(message)
	}
	return output, nil
}

func (m Manager) runner() Runner {
	if m.Runner != nil {
		return m.Runner
	}
	return ExecRunner{}
}

func (m Manager) executable() (string, error) {
	if m.Executable != "" {
		return filepath.Abs(m.Executable)
	}
	return os.Executable()
}

func sortedTargets(targets map[string]config.Target) []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func clientOnProfile(list, tty, profile string) bool {
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) == 2 && parts[0] == tty && parts[1] == profile {
			return true
		}
	}
	return false
}
