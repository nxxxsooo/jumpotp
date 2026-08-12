package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion   = 1
	DefaultLauncher = "ssh"
	DefaultProvider = "bitwarden"
	DefaultFallback = "prompt"
	DefaultInterval = 4 * time.Minute
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return fmt.Errorf("duration must be a string")
	}
	value, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}
	d.Duration = value
	return nil
}

type Config struct {
	Path     string             `yaml:"-"`
	Version  int                `yaml:"version"`
	Defaults Defaults           `yaml:"defaults,omitempty"`
	Probes   []Probe            `yaml:"probes,omitempty"`
	Profiles map[string]Profile `yaml:"profiles"`
}

type Defaults struct {
	Launcher string      `yaml:"launcher,omitempty"`
	OTP      OTPDefaults `yaml:"otp,omitempty"`
}

type OTPDefaults struct {
	Provider string `yaml:"provider,omitempty"`
	Fallback string `yaml:"fallback,omitempty"`
}

type Probe struct {
	ID       string `yaml:"id"`
	Enabled  *bool  `yaml:"enabled"`
	Label    string `yaml:"label"`
	Platform string `yaml:"platform"`
	Command  string `yaml:"command"`
}

type Profile struct {
	Launcher  string            `yaml:"launcher,omitempty"`
	MFA       MFA               `yaml:"mfa"`
	OTP       OTP               `yaml:"otp"`
	Targets   map[string]Target `yaml:"targets"`
	Workspace *Workspace        `yaml:"workspace,omitempty"`
}

type MFA struct {
	Preset  string `yaml:"preset"`
	Pattern string `yaml:"pattern,omitempty"`
	Digits  *int   `yaml:"digits,omitempty"`
}

type OTP struct {
	Provider string `yaml:"provider,omitempty"`
	Item     string `yaml:"item"`
	Fallback string `yaml:"fallback,omitempty"`
}

type Target struct {
	SSH string     `yaml:"ssh"`
	OTP *TargetOTP `yaml:"otp,omitempty"`
}

type TargetOTP struct {
	Item string `yaml:"item"`
}

type Workspace struct {
	Health *Health `yaml:"health,omitempty"`
}

type Health struct {
	Enabled  bool     `yaml:"enabled,omitempty"`
	Interval Duration `yaml:"interval,omitempty"`
	Commands []string `yaml:"commands,omitempty"`
}

type Problem struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type EffectiveTarget struct {
	Profile  string
	Target   string
	SSH      string
	Launcher string
	Provider string
	Item     string
	Fallback string
	MFA      MFA
	Manual   bool
	Master   bool
	Health   EffectiveHealth
}

type EffectiveHealth struct {
	Enabled  bool
	Interval time.Duration
	Probes   []Probe
}

func (c *Config) ApplyDefaults() {
	if c.Defaults.Launcher == "" {
		c.Defaults.Launcher = DefaultLauncher
	}
	if c.Defaults.OTP.Provider == "" {
		c.Defaults.OTP.Provider = DefaultProvider
	}
	if c.Defaults.OTP.Fallback == "" {
		c.Defaults.OTP.Fallback = DefaultFallback
	}
	for name, profile := range c.Profiles {
		if profile.Launcher == "" {
			profile.Launcher = c.Defaults.Launcher
		}
		if profile.OTP.Provider == "" {
			profile.OTP.Provider = c.Defaults.OTP.Provider
		}
		if profile.OTP.Fallback == "" {
			profile.OTP.Fallback = c.Defaults.OTP.Fallback
		}
		if profile.Workspace != nil && profile.Workspace.Health != nil && profile.Workspace.Health.Interval.Duration == 0 {
			profile.Workspace.Health.Interval.Duration = DefaultInterval
		}
		c.Profiles[name] = profile
	}
}

func (c *Config) Validate() []Problem {
	var problems []Problem
	add := func(code, path, message string) {
		problems = append(problems, Problem{Code: code, Path: path, Message: message})
	}

	if c.Version != SchemaVersion {
		add("unsupported_version", "version", "must be exactly 1")
	}
	if !validLauncher(c.Defaults.Launcher) {
		add("invalid_launcher", "defaults.launcher", "must be ssh or sshm")
	}
	if c.Defaults.OTP.Provider != DefaultProvider {
		add("unsupported_provider", "defaults.otp.provider", "v0.1 supports only bitwarden")
	}
	if !validFallback(c.Defaults.OTP.Fallback) {
		add("invalid_fallback", "defaults.otp.fallback", "must be prompt or fail")
	}

	probeByID := make(map[string]Probe, len(c.Probes))
	for index, probe := range c.Probes {
		path := fmt.Sprintf("probes[%d]", index)
		if !validIdentifier(probe.ID) {
			add("invalid_identifier", path+".id", "must match [A-Za-z0-9][A-Za-z0-9._-]*")
		} else if _, exists := probeByID[probe.ID]; exists {
			add("duplicate_probe", path+".id", "probe IDs must be unique")
		} else {
			probeByID[probe.ID] = probe
		}
		if probe.Enabled == nil {
			add("required", path+".enabled", "is required")
		}
		if strings.TrimSpace(probe.Label) == "" {
			add("required", path+".label", "is required")
		}
		if probe.Platform != "linux" && probe.Platform != "darwin" && probe.Platform != "any" {
			add("invalid_platform", path+".platform", "must be linux, darwin, or any")
		}
		if strings.TrimSpace(probe.Command) == "" {
			add("required", path+".command", "is required")
		}
	}

	if len(c.Profiles) == 0 {
		add("required", "profiles", "must contain at least one profile")
	}
	profileNames := sortedKeys(c.Profiles)
	for _, profileName := range profileNames {
		profile := c.Profiles[profileName]
		base := "profiles." + profileName
		if !validIdentifier(profileName) {
			add("invalid_identifier", base, "must match [A-Za-z0-9][A-Za-z0-9._-]*")
		}
		if !validLauncher(profile.Launcher) {
			add("invalid_launcher", base+".launcher", "must be ssh or sshm")
		}
		validateMFA(profile.MFA, base+".mfa", add)
		if profile.OTP.Provider != DefaultProvider {
			add("unsupported_provider", base+".otp.provider", "v0.1 supports only bitwarden")
		}
		if strings.TrimSpace(profile.OTP.Item) == "" {
			add("required", base+".otp.item", "is required")
		}
		if !validFallback(profile.OTP.Fallback) {
			add("invalid_fallback", base+".otp.fallback", "must be prompt or fail")
		}
		if len(profile.Targets) == 0 {
			add("required", base+".targets", "must contain at least one target")
		}
		targetNames := sortedKeys(profile.Targets)
		for _, targetName := range targetNames {
			target := profile.Targets[targetName]
			targetBase := base + ".targets." + targetName
			if !validIdentifier(targetName) {
				add("invalid_identifier", targetBase, "must match [A-Za-z0-9][A-Za-z0-9._-]*")
			}
			if !validIdentifier(target.SSH) {
				add("invalid_ssh_alias", targetBase+".ssh", "must match [A-Za-z0-9][A-Za-z0-9._-]*")
			}
			if target.OTP != nil && strings.TrimSpace(target.OTP.Item) == "" {
				add("required", targetBase+".otp.item", "is required")
			}
		}
		if profile.Workspace != nil && profile.Workspace.Health != nil {
			health := profile.Workspace.Health
			if health.Interval.Duration < 30*time.Second || health.Interval.Duration > 24*time.Hour {
				add("invalid_interval", base+".workspace.health.interval", "must be from 30s through 24h")
			}
			seen := map[string]bool{}
			commands := health.Commands
			if commands == nil {
				for _, probe := range c.Probes {
					if probe.Enabled != nil && *probe.Enabled {
						commands = append(commands, probe.ID)
					}
				}
			}
			for index, id := range commands {
				path := fmt.Sprintf("%s.workspace.health.commands[%d]", base, index)
				if seen[id] {
					add("duplicate_probe_reference", path, "probe IDs must be unique in a profile")
					continue
				}
				seen[id] = true
				probe, ok := probeByID[id]
				if !ok {
					add("unknown_probe", path, "must reference a defined probe")
				} else if probe.Enabled == nil || !*probe.Enabled {
					add("disabled_probe", path, "must reference an enabled probe")
				}
			}
		}
	}

	sort.SliceStable(problems, func(i, j int) bool {
		if problems[i].Path == problems[j].Path {
			if problems[i].Code == problems[j].Code {
				return problems[i].Message < problems[j].Message
			}
			return problems[i].Code < problems[j].Code
		}
		return problems[i].Path < problems[j].Path
	})
	return problems
}

func (c *Config) Resolve(profileName, targetName, launcherOverride string, manual bool) (EffectiveTarget, error) {
	profile, ok := c.Profiles[profileName]
	if !ok {
		return EffectiveTarget{}, fmt.Errorf("profile %q is not configured", profileName)
	}
	target, ok := profile.Targets[targetName]
	if !ok {
		return EffectiveTarget{}, fmt.Errorf("target %q is not configured in profile %q", targetName, profileName)
	}
	launcher := profile.Launcher
	if launcherOverride != "" {
		if !validLauncher(launcherOverride) {
			return EffectiveTarget{}, fmt.Errorf("launcher must be ssh or sshm")
		}
		launcher = launcherOverride
	}
	item := profile.OTP.Item
	if target.OTP != nil {
		item = target.OTP.Item
	}
	effective := EffectiveTarget{
		Profile:  profileName,
		Target:   targetName,
		SSH:      target.SSH,
		Launcher: launcher,
		Provider: profile.OTP.Provider,
		Item:     item,
		Fallback: profile.OTP.Fallback,
		MFA:      profile.MFA,
		Manual:   manual,
	}
	if profile.Workspace != nil && profile.Workspace.Health != nil {
		health := profile.Workspace.Health
		effective.Health.Enabled = health.Enabled
		effective.Health.Interval = health.Interval.Duration
		byID := make(map[string]Probe, len(c.Probes))
		for _, probe := range c.Probes {
			byID[probe.ID] = probe
		}
		commands := health.Commands
		if commands == nil {
			for _, probe := range c.Probes {
				if probe.Enabled != nil && *probe.Enabled {
					commands = append(commands, probe.ID)
				}
			}
		}
		for _, id := range commands {
			effective.Health.Probes = append(effective.Health.Probes, byID[id])
		}
	}
	return effective, nil
}

func ParseAddress(value string) (string, string, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validIdentifier(parts[0]) || !validIdentifier(parts[1]) {
		return "", "", fmt.Errorf("target must use PROFILE/TARGET with valid identifiers")
	}
	return parts[0], parts[1], nil
}

func validateMFA(mfa MFA, path string, add func(string, string, string)) {
	switch mfa.Preset {
	case "jumpserver-koko":
		if mfa.Pattern != "" {
			add("mutually_exclusive", path+".pattern", "is allowed only with preset custom")
		}
		if mfa.Digits != nil && *mfa.Digits != 6 {
			add("invalid_digits", path+".digits", "jumpserver-koko requires exactly 6")
		}
	case "generic-totp":
		if mfa.Pattern != "" {
			add("mutually_exclusive", path+".pattern", "is allowed only with preset custom")
		}
	case "custom":
		if strings.TrimSpace(mfa.Pattern) == "" {
			add("required", path+".pattern", "is required for preset custom")
		} else if len([]byte(mfa.Pattern)) > 4096 {
			add("too_long", path+".pattern", "must be at most 4096 UTF-8 bytes")
		}
	default:
		add("invalid_mfa_preset", path+".preset", "must be jumpserver-koko, generic-totp, or custom")
	}
	if mfa.Digits != nil && (*mfa.Digits < 5 || *mfa.Digits > 10) {
		add("invalid_digits", path+".digits", "must be from 5 through 10")
	}
}

func validIdentifier(value string) bool {
	return identifierPattern.MatchString(value)
}

func validLauncher(value string) bool {
	return value == "ssh" || value == "sshm"
}

func validFallback(value string) bool {
	return value == "prompt" || value == "fail"
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
