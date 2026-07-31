package doctor

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

const checkTimeout = 5 * time.Second

type Check struct {
	Name   string `json:"name"`
	Target string `json:"target,omitempty"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type Report struct {
	SchemaVersion int     `json:"schema_version"`
	OK            bool    `json:"ok"`
	Checks        []Check `json:"checks"`
}

type Runner interface {
	LookPath(string) (string, error)
	Output(context.Context, string, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func Run(ctx context.Context, cfg *config.Config, profileFilter string, runner Runner) Report {
	report := Report{SchemaVersion: 1, OK: true, Checks: []Check{}}
	profiles := selectedProfiles(cfg, profileFilter)
	executables := map[string][]string{
		"ssh":  {"-V"},
		"bw":   {"--version"},
		"tmux": {"-V"},
	}
	for _, profileName := range profiles {
		if cfg.Profiles[profileName].Launcher == "sshm" {
			executables["sshm"] = []string{"--version"}
		}
	}
	names := make([]string, 0, len(executables))
	for name := range executables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path, err := runner.LookPath(name)
		if err != nil {
			report.add(Check{Name: "executable", Target: name, Detail: "not found"})
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		output, versionErr := runner.Output(checkCtx, name, executables[name]...)
		cancel()
		detail := "available"
		ok := true
		if versionErr != nil {
			ok = false
			detail = "version check failed"
		} else if value := firstLine(output); value != "" {
			detail = value
		}
		report.add(Check{Name: "executable", Target: name, OK: ok, Detail: detail + " (" + path + ")"})
	}

	type aliasEntry struct {
		profile string
		target  string
		alias   string
	}
	var aliases []aliasEntry
	for _, profileName := range profiles {
		profile := cfg.Profiles[profileName]
		targetNames := make([]string, 0, len(profile.Targets))
		for targetName := range profile.Targets {
			targetNames = append(targetNames, targetName)
		}
		sort.Strings(targetNames)
		for _, targetName := range targetNames {
			aliases = append(aliases, aliasEntry{profile: profileName, target: targetName, alias: profile.Targets[targetName].SSH})
		}
	}
	for _, entry := range aliases {
		checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		_, err := runner.Output(checkCtx, "ssh", "-G", entry.alias)
		cancel()
		check := Check{
			Name:   "ssh-expansion",
			Target: entry.profile + "/" + entry.target,
			OK:     err == nil,
			Detail: "non-connecting expansion succeeded",
		}
		if err != nil {
			check.Detail = "non-connecting expansion failed"
		}
		report.add(check)
	}
	return report
}

func (r *Report) add(check Check) {
	r.Checks = append(r.Checks, check)
	if !check.OK {
		r.OK = false
	}
}

func selectedProfiles(cfg *config.Config, filter string) []string {
	var names []string
	if filter != "" {
		return []string{filter}
	}
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func firstLine(output []byte) string {
	value := strings.TrimSpace(string(output))
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	if len(value) > 160 {
		value = value[:160]
	}
	if value == "" {
		return ""
	}
	return fmt.Sprintf("%s", value)
}
