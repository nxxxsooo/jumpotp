package probes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

const maxProbeOutput = 64 * 1024

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var output limitedWriter
	output.limit = maxProbeOutput
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return append([]byte(nil), output.buffer.Bytes()...), err
}

type Scheduler struct {
	Config  *config.Config
	Profile string
	Runner  Runner
	Out     io.Writer
}

func (s Scheduler) Run(ctx context.Context) error {
	runner := s.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	out := s.Out
	if out == nil {
		out = io.Discard
	}
	profile, ok := s.Config.Profiles[s.Profile]
	if !ok {
		return fmt.Errorf("profile %q is not configured", s.Profile)
	}
	if profile.Workspace == nil || profile.Workspace.Health == nil || !profile.Workspace.Health.Enabled {
		fmt.Fprintln(out, "JumpOTP health probes are disabled.")
		return nil
	}
	targetNames := make([]string, 0, len(profile.Targets))
	for name := range profile.Targets {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)
	if len(targetNames) == 0 {
		return nil
	}
	first, err := s.Config.Resolve(s.Profile, targetNames[0], "", false)
	if err != nil {
		return err
	}
	probeList := first.Health.Probes
	if len(probeList) == 0 {
		fmt.Fprintln(out, "JumpOTP health probe list is empty.")
		return nil
	}
	interval := first.Health.Interval
	if interval <= 0 {
		interval = config.DefaultInterval
	}
	index := 0
	for {
		targetName := targetNames[index%len(targetNames)]
		probe := probeList[(index/len(targetNames))%len(probeList)]
		target := profile.Targets[targetName]
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, checkErr := runner.Run(checkCtx, "ssh", "-O", "check", target.SSH)
		cancel()
		if checkErr != nil {
			fmt.Fprintf(out, "[%s/%s] skipped: no confirmed ControlMaster\n", s.Profile, targetName)
		} else {
			probeCtx, probeCancel := context.WithTimeout(ctx, 20*time.Second)
			output, probeErr := runner.Run(
				probeCtx,
				"ssh",
				"-o", "BatchMode=yes",
				"-o", "ConnectTimeout=10",
				target.SSH,
				probe.Command,
			)
			probeCancel()
			state := "ok"
			if probeErr != nil {
				state = "failed"
			}
			fmt.Fprintf(out, "\n[%s/%s] %s — %s\n", s.Profile, targetName, probe.Label, state)
			_, _ = out.Write(output)
			if len(output) > 0 && output[len(output)-1] != '\n' {
				fmt.Fprintln(out)
			}
		}
		index++
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

type limitedWriter struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (w *limitedWriter) Write(value []byte) (int, error) {
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.exceeded = true
		return len(value), nil
	}
	if len(value) > remaining {
		w.exceeded = true
		_, _ = w.buffer.Write(value[:remaining])
		return len(value), nil
	}
	_, _ = w.buffer.Write(value)
	return len(value), nil
}
