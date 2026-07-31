package workspace

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	runtimepath "github.com/nxxxsooo/jumpotp/internal/runtime"
)

type StatusReport struct {
	SchemaVersion int               `json:"schema_version"`
	Workspaces    []WorkspaceStatus `json:"workspaces"`
}

type WorkspaceStatus struct {
	Profile         string         `json:"profile"`
	State           string         `json:"state"`
	AttachedClients int            `json:"attached_clients"`
	Broker          string         `json:"broker"`
	Targets         []TargetStatus `json:"targets"`
	Health          string         `json:"health"`
}

type TargetStatus struct {
	Target        string `json:"target"`
	State         string `json:"state"`
	ControlMaster string `json:"control_master"`
}

func (m Manager) Status(ctx context.Context, profileFilter string) (StatusReport, error) {
	report := StatusReport{SchemaVersion: 1, Workspaces: []WorkspaceStatus{}}
	socket, _, err := m.Paths()
	if err != nil {
		return report, err
	}
	state, stateErr := m.checkServer(ctx, socket)
	if state == serverAbsent {
		return report, nil
	}
	if state == serverUnhealthy {
		return report, fmt.Errorf("JumpOTP tmux server is unhealthy: %w", stateErr)
	}
	output, err := m.command(ctx, socket, "list-sessions", "-F", "#{session_name}\t#{session_attached}")
	if err != nil {
		return report, err
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 2 || parts[0] == "" {
			return report, fmt.Errorf("tmux returned invalid session status")
		}
		profile := parts[0]
		if profileFilter != "" && profile != profileFilter {
			continue
		}
		profileConfig, configured := m.Config.Profiles[profile]
		if !configured {
			continue
		}
		attached, _ := strconv.Atoi(parts[1])
		status := WorkspaceStatus{
			Profile:         profile,
			State:           "detached",
			AttachedClients: attached,
			Broker:          brokerStateForProfile(profile),
			Targets:         []TargetStatus{},
			Health:          "disabled",
		}
		if attached > 0 {
			status.State = "running"
		}
		windowOutput, err := m.command(ctx, socket, "list-windows", "-t", profile, "-F", "#{window_name}\t#{pane_dead}\t#{@jumpotp_target}\t#{@jumpotp_health}")
		if err != nil {
			status.State = "failed"
			report.Workspaces = append(report.Workspaces, status)
			continue
		}
		windowByTarget := map[string][]string{}
		for _, windowLine := range strings.Split(strings.TrimSuffix(string(windowOutput), "\n"), "\n") {
			fields := strings.Split(windowLine, "\t")
			if len(fields) == 4 {
				windowByTarget[fields[0]] = fields
				if fields[3] == "1" {
					status.Health = "running"
					if fields[1] == "1" {
						status.Health = "failed"
					}
				}
			}
		}
		targetNames := sortedTargets(profileConfig.Targets)
		for _, targetName := range targetNames {
			targetState := "stopped"
			if fields, ok := windowByTarget[targetName]; ok {
				if fields[1] == "1" || fields[2] != targetName {
					targetState = "failed"
				} else {
					targetState = "running"
				}
			}
			master := m.controlMasterState(ctx, profileConfig.Targets[targetName].SSH)
			status.Targets = append(status.Targets, TargetStatus{Target: targetName, State: targetState, ControlMaster: master})
		}
		if profileConfig.Workspace != nil && profileConfig.Workspace.Health != nil && profileConfig.Workspace.Health.Enabled && status.Health == "disabled" {
			status.Health = "failed"
		}
		report.Workspaces = append(report.Workspaces, status)
	}
	sort.Slice(report.Workspaces, func(i, j int) bool { return report.Workspaces[i].Profile < report.Workspaces[j].Profile })
	return report, nil
}

func (m Manager) controlMasterState(ctx context.Context, alias string) string {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := m.runner().Output(checkCtx, "ssh", "-O", "check", alias); err != nil {
		return "unavailable"
	}
	return "available"
}

func brokerStateForProfile(profile string) string {
	path, err := brokerSocketPathForStatus()
	if err != nil {
		return "unavailable"
	}
	path = strings.TrimSuffix(path, "broker-.sock") + "broker-" + profile + ".sock"
	connection, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return "unavailable"
	}
	connection.Close()
	return "available"
}

func brokerSocketPathForStatus() (string, error) {
	dir, err := runtimepath.SecureDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "broker-.sock"), nil
}
