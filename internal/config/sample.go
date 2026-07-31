package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const Sample = `# JumpOTP configuration version 1.
# Values below are synthetic. Keep real item references in your private config.
version: 1

defaults:
  launcher: ssh
  otp:
    provider: bitwarden
    fallback: prompt

# Probe definitions are editable and ordered. Health execution remains disabled
# in the profile until you opt in.
probes:
  - id: uptime
    enabled: true
    label: Time, load, and uptime
    platform: linux
    command: "date '+%F %T %Z'; uptime"
  - id: memory
    enabled: true
    label: Memory and swap
    platform: linux
    command: "free -h"
  - id: root-filesystem
    enabled: true
    label: Root filesystem
    platform: linux
    command: "df -h /"
  - id: failed-units
    enabled: true
    label: Failed systemd units
    platform: linux
    command: "systemctl --failed --no-legend --no-pager"
  - id: network-summary
    enabled: true
    label: Network summary
    platform: linux
    command: "ss -s"
  - id: top-cpu
    enabled: true
    label: Top CPU processes
    platform: linux
    command: "LC_ALL=C ps -eo pid,comm,%cpu,%mem,etime --sort=-%cpu | head -n 8"

profiles:
  production:
    launcher: ssh
    mfa:
      preset: jumpserver-koko
    otp:
      provider: bitwarden
      item: Example Login
      fallback: prompt
    targets:
      app-01:
        ssh: production-app-01
      app-02:
        ssh: production-app-02
        otp:
          item: Example Login Override
    workspace:
      health:
        enabled: false
        interval: 4m
        commands:
          - uptime
          - memory
          - root-filesystem
          - failed-units
          - network-summary
          - top-cpu
`

func WriteSample(path string, force bool) error {
	if !force {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("configuration already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect configuration path: %w", err)
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure configuration directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := temp.WriteString(Sample); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if !force {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("configuration already exists: %s", path)
		}
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("install configuration: %w", err)
	}
	return nil
}
