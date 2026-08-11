package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/broker"
	"github.com/nxxxsooo/jumpotp/internal/config"
	"github.com/nxxxsooo/jumpotp/internal/doctor"
	"github.com/nxxxsooo/jumpotp/internal/provider"
	terminalproxy "github.com/nxxxsooo/jumpotp/internal/terminal"
	"github.com/nxxxsooo/jumpotp/internal/version"
	"github.com/nxxxsooo/jumpotp/internal/workspace"
)

const (
	ExitOK         = 0
	ExitUsage      = 2
	ExitDependency = 3
	ExitMFA        = 4
	ExitWorkspace  = 5
	ExitInterrupt  = 130
)

type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type Handlers struct {
	Connect   func(*config.Config, config.EffectiveTarget, Streams) int
	Workspace func(*config.Config, string, string, bool, Streams) int
	Status    func(*config.Config, string, bool, Streams) int
	Stop      func(*config.Config, string, Streams) int
	Doctor    func(*config.Config, string, bool, Streams) int
}

func Run(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) > 0 && strings.HasPrefix(args[0], "__") {
		return runInternal(args, Streams{In: in, Out: out, Err: errOut})
	}
	return RunWithHandlers(args, Streams{In: in, Out: out, Err: errOut}, defaultHandlers())
}

func RunWithHandlers(args []string, streams Streams, handlers Handlers) int {
	configPath, command, rest, err := parseGlobal(args)
	if err != nil {
		return usageError(streams.Err, err)
	}
	switch command {
	case "help":
		return runHelp(rest, streams)
	case "version":
		return runVersion(rest, streams)
	case "config":
		return runConfig(configPath, rest, streams)
	case "connect":
		return runConnect(configPath, rest, streams, handlers)
	case "workspace":
		return runWorkspace(configPath, rest, streams, handlers)
	case "status":
		return runStatus(configPath, rest, streams, handlers)
	case "stop":
		return runStop(configPath, rest, streams, handlers)
	case "doctor":
		return runDoctor(configPath, rest, streams, handlers)
	default:
		return usageError(streams.Err, fmt.Errorf("unknown command %q", command))
	}
}

func parseGlobal(args []string) (string, string, []string, error) {
	if len(args) == 0 {
		return "", "", nil, errors.New("missing command")
	}
	var configPath string
	index := 0
	for index < len(args) {
		switch args[index] {
		case "--config":
			if configPath != "" {
				return "", "", nil, errors.New("--config may be supplied only once")
			}
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
				return "", "", nil, errors.New("--config requires PATH")
			}
			configPath = args[index+1]
			index += 2
		case "-h", "--help":
			return configPath, "help", nil, nil
		default:
			if strings.HasPrefix(args[index], "-") {
				return "", "", nil, fmt.Errorf("unknown global flag %q", args[index])
			}
			return configPath, args[index], args[index+1:], nil
		}
	}
	return "", "", nil, errors.New("missing command")
}

func runHelp(args []string, streams Streams) int {
	if len(args) > 1 {
		return usageError(streams.Err, errors.New("help accepts at most one command"))
	}
	if len(args) == 1 && !knownCommand(args[0]) {
		return usageError(streams.Err, fmt.Errorf("unknown command %q", args[0]))
	}
	if len(args) == 1 {
		fmt.Fprintln(streams.Out, commandUsage(args[0]))
		return ExitOK
	}
	fmt.Fprintln(streams.Out, `JumpOTP safely assists authorized interactive SSH TOTP prompts.

Usage:
  jumpotp [--config PATH] connect PROFILE/TARGET [--launcher ssh|sshm] [--manual]
  jumpotp [--config PATH] workspace PROFILE [--launcher ssh|sshm] [--manual]
  jumpotp [--config PATH] status [PROFILE] [--json]
  jumpotp [--config PATH] stop PROFILE
  jumpotp [--config PATH] doctor [PROFILE] [--json]
  jumpotp [--config PATH] config init [--force]
  jumpotp [--config PATH] config validate [PATH] [--json]
  jumpotp version [--json]
  jumpotp help [COMMAND]`)
	return ExitOK
}

func runVersion(args []string, streams Streams) int {
	jsonMode, positionals, err := parseBooleanFlag(args, "--json")
	if err != nil || len(positionals) != 0 {
		if err == nil {
			err = errors.New("version accepts only --json")
		}
		return usageError(streams.Err, err)
	}
	if jsonMode {
		return writeJSON(streams.Out, map[string]any{
			"schema_version": 1,
			"name":           "jumpotp",
			"version":        version.Value,
			"platform":       stdruntime.GOOS,
			"arch":           stdruntime.GOARCH,
		}, streams.Err)
	}
	fmt.Fprintf(streams.Out, "jumpotp %s (%s/%s)\n", version.Value, stdruntime.GOOS, stdruntime.GOARCH)
	return ExitOK
}

func runConfig(globalPath string, args []string, streams Streams) int {
	if len(args) == 0 {
		return usageError(streams.Err, errors.New("config requires init or validate"))
	}
	subcommand := args[0]
	switch subcommand {
	case "init":
		force, positionals, err := parseBooleanFlag(args[1:], "--force")
		if err != nil || len(positionals) != 0 {
			if err == nil {
				err = errors.New("config init accepts only --force")
			}
			return usageError(streams.Err, err)
		}
		path, err := config.Discover(globalPath)
		if err != nil {
			return configError(streams.Err, err)
		}
		if err := config.WriteSample(path, force); err != nil {
			return configError(streams.Err, err)
		}
		fmt.Fprintf(streams.Out, "Created %s\n", path)
		return ExitOK
	case "validate":
		jsonMode, positionals, err := parseBooleanFlag(args[1:], "--json")
		if err != nil || len(positionals) > 1 {
			if err == nil {
				err = errors.New("config validate accepts at most one PATH")
			}
			return usageError(streams.Err, err)
		}
		if globalPath != "" && len(positionals) == 1 {
			return usageError(streams.Err, errors.New("global --config conflicts with config validate PATH"))
		}
		pathInput := globalPath
		if len(positionals) == 1 {
			pathInput = positionals[0]
		}
		path, err := config.Discover(pathInput)
		if err != nil {
			return renderValidation(streams, jsonMode, "", err)
		}
		_, err = config.Load(path)
		return renderValidation(streams, jsonMode, path, err)
	default:
		return usageError(streams.Err, fmt.Errorf("unknown config command %q", subcommand))
	}
}

func runConnect(globalPath string, args []string, streams Streams, handlers Handlers) int {
	launcher, manual, positionals, err := parseInteractiveOptions(args)
	if err != nil || len(positionals) != 1 {
		if err == nil {
			err = errors.New("connect requires exactly PROFILE/TARGET")
		}
		return usageError(streams.Err, err)
	}
	profile, target, err := config.ParseAddress(positionals[0])
	if err != nil {
		return usageError(streams.Err, err)
	}
	cfg, code := loadConfig(globalPath, streams.Err)
	if code != ExitOK {
		return code
	}
	effective, err := cfg.Resolve(profile, target, launcher, manual)
	if err != nil {
		return usageError(streams.Err, err)
	}
	return handlers.Connect(cfg, effective, streams)
}

func runWorkspace(globalPath string, args []string, streams Streams, handlers Handlers) int {
	launcher, manual, positionals, err := parseInteractiveOptions(args)
	if err != nil || len(positionals) != 1 {
		if err == nil {
			err = errors.New("workspace requires exactly PROFILE")
		}
		return usageError(streams.Err, err)
	}
	profile := positionals[0]
	if !validSingleIdentifier(profile) {
		return usageError(streams.Err, errors.New("workspace profile is invalid"))
	}
	cfg, code := loadConfig(globalPath, streams.Err)
	if code != ExitOK {
		return code
	}
	if _, ok := cfg.Profiles[profile]; !ok {
		return usageError(streams.Err, fmt.Errorf("profile %q is not configured", profile))
	}
	return handlers.Workspace(cfg, profile, launcher, manual, streams)
}

func runStatus(globalPath string, args []string, streams Streams, handlers Handlers) int {
	jsonMode, positionals, err := parseBooleanFlag(args, "--json")
	if err != nil || len(positionals) > 1 {
		if err == nil {
			err = errors.New("status accepts at most one PROFILE")
		}
		return usageError(streams.Err, err)
	}
	profile := ""
	if len(positionals) == 1 {
		profile = positionals[0]
		if !validSingleIdentifier(profile) {
			return usageError(streams.Err, errors.New("status profile is invalid"))
		}
	}
	cfg, code := loadConfig(globalPath, streams.Err)
	if code != ExitOK {
		return code
	}
	return handlers.Status(cfg, profile, jsonMode, streams)
}

func runStop(globalPath string, args []string, streams Streams, handlers Handlers) int {
	if len(args) != 1 || !validSingleIdentifier(args[0]) {
		return usageError(streams.Err, errors.New("stop requires exactly one valid PROFILE"))
	}
	cfg, code := loadConfig(globalPath, streams.Err)
	if code != ExitOK {
		return code
	}
	if _, ok := cfg.Profiles[args[0]]; !ok {
		return usageError(streams.Err, fmt.Errorf("profile %q is not configured", args[0]))
	}
	return handlers.Stop(cfg, args[0], streams)
}

func runDoctor(globalPath string, args []string, streams Streams, handlers Handlers) int {
	jsonMode, positionals, err := parseBooleanFlag(args, "--json")
	if err != nil || len(positionals) > 1 {
		if err == nil {
			err = errors.New("doctor accepts at most one PROFILE")
		}
		return usageError(streams.Err, err)
	}
	profile := ""
	if len(positionals) == 1 {
		profile = positionals[0]
		if !validSingleIdentifier(profile) {
			return usageError(streams.Err, errors.New("doctor profile is invalid"))
		}
	}
	cfg, code := loadConfig(globalPath, streams.Err)
	if code != ExitOK {
		return code
	}
	if profile != "" {
		if _, ok := cfg.Profiles[profile]; !ok {
			return usageError(streams.Err, fmt.Errorf("profile %q is not configured", profile))
		}
	}
	return handlers.Doctor(cfg, profile, jsonMode, streams)
}

func loadConfig(explicit string, errOut io.Writer) (*config.Config, int) {
	path, err := config.Discover(explicit)
	if err != nil {
		return nil, configError(errOut, err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, configError(errOut, err)
	}
	return cfg, ExitOK
}

func renderValidation(streams Streams, jsonMode bool, path string, err error) int {
	if jsonMode {
		result := struct {
			SchemaVersion int              `json:"schema_version"`
			Valid         bool             `json:"valid"`
			Path          string           `json:"path,omitempty"`
			Errors        []config.Problem `json:"errors"`
		}{SchemaVersion: 1, Valid: err == nil, Path: path, Errors: []config.Problem{}}
		if err != nil {
			var validation *config.ValidationError
			if errors.As(err, &validation) {
				result.Errors = validation.Problems
			} else {
				result.Errors = []config.Problem{{Code: "config_error", Path: "$", Message: err.Error()}}
			}
		}
		if writeJSON(streams.Out, result, streams.Err) != ExitOK {
			return ExitUsage
		}
		if err != nil {
			return ExitUsage
		}
		return ExitOK
	}
	if err != nil {
		return configError(streams.Err, err)
	}
	fmt.Fprintf(streams.Out, "Valid configuration: %s\n", path)
	return ExitOK
}

func parseInteractiveOptions(args []string) (string, bool, []string, error) {
	var launcher string
	var manual bool
	var positionals []string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--launcher":
			if launcher != "" {
				return "", false, nil, errors.New("--launcher may be supplied only once")
			}
			if index+1 >= len(args) {
				return "", false, nil, errors.New("--launcher requires ssh or sshm")
			}
			launcher = args[index+1]
			if launcher != "ssh" && launcher != "sshm" {
				return "", false, nil, errors.New("--launcher requires ssh or sshm")
			}
			index++
		case "--manual":
			if manual {
				return "", false, nil, errors.New("--manual may be supplied only once")
			}
			manual = true
		default:
			if strings.HasPrefix(args[index], "-") {
				return "", false, nil, fmt.Errorf("unknown flag %q", args[index])
			}
			positionals = append(positionals, args[index])
		}
	}
	return launcher, manual, positionals, nil
}

func parseBooleanFlag(args []string, flag string) (bool, []string, error) {
	found := false
	var positionals []string
	for _, arg := range args {
		if arg == flag {
			if found {
				return false, nil, fmt.Errorf("%s may be supplied only once", flag)
			}
			found = true
		} else if strings.HasPrefix(arg, "-") {
			return false, nil, fmt.Errorf("unknown flag %q", arg)
		} else {
			positionals = append(positionals, arg)
		}
	}
	return found, positionals, nil
}

func renderProblems(err error) string {
	var validation *config.ValidationError
	if !errors.As(err, &validation) {
		return err.Error()
	}
	var lines []string
	for _, problem := range validation.Problems {
		lines = append(lines, fmt.Sprintf("%s [%s]: %s", problem.Path, problem.Code, problem.Message))
	}
	return strings.Join(lines, "\n")
}

func usageError(out io.Writer, err error) int {
	fmt.Fprintf(out, "jumpotp: %s\nRun 'jumpotp help' for usage.\n", err)
	return ExitUsage
}

func configError(out io.Writer, err error) int {
	fmt.Fprintf(out, "jumpotp: %s\n", renderProblems(err))
	return ExitUsage
}

func writeJSON(out io.Writer, value any, errOut io.Writer) int {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(errOut, "jumpotp: encode JSON: %v\n", err)
		return ExitUsage
	}
	return ExitOK
}

func knownCommand(value string) bool {
	switch value {
	case "connect", "workspace", "status", "stop", "doctor", "config", "version", "help":
		return true
	default:
		return false
	}
}

func commandUsage(command string) string {
	switch command {
	case "connect":
		return "Usage: jumpotp [--config PATH] connect PROFILE/TARGET [--launcher ssh|sshm] [--manual]"
	case "workspace":
		return "Usage: jumpotp [--config PATH] workspace PROFILE [--launcher ssh|sshm] [--manual]"
	case "status":
		return "Usage: jumpotp [--config PATH] status [PROFILE] [--json]"
	case "stop":
		return "Usage: jumpotp [--config PATH] stop PROFILE"
	case "doctor":
		return "Usage: jumpotp [--config PATH] doctor [PROFILE] [--json]"
	case "config":
		return "Usage: jumpotp [--config PATH] config init [--force]\n       jumpotp [--config PATH] config validate [PATH] [--json]"
	case "version":
		return "Usage: jumpotp version [--json]"
	default:
		return "Usage: jumpotp help [COMMAND]"
	}
}

func validSingleIdentifier(value string) bool {
	_, _, err := config.ParseAddress(value + "/target")
	return err == nil && !strings.Contains(value, string(filepath.Separator))
}

func runDirectWithReadiness(ctx context.Context, manual bool, source provider.ReadinessSource, errOut io.Writer, launch func() int) int {
	if !manual {
		warnReadinessFailure(source.Ready(ctx), errOut)
	}
	return launch()
}

func runWorkspaceWithReadiness(
	ctx context.Context,
	profile string,
	manual bool,
	source provider.ReadinessSource,
	errOut io.Writer,
	active func(string) (bool, error),
	launch func() int,
) int {
	if !manual {
		brokerActive, err := active(profile)
		if err == nil && !brokerActive {
			warnReadinessFailure(source.Ready(ctx), errOut)
		}
	}
	return launch()
}

func warnReadinessFailure(err error, errOut io.Writer) {
	if err != nil {
		fmt.Fprintf(errOut, "jumpotp: %s; continuing with configured MFA fallback\n", provider.SafeReadinessMessage(err))
	}
}

func defaultHandlers() Handlers {
	return Handlers{
		Connect: func(_ *config.Config, target config.EffectiveTarget, streams Streams) int {
			input, ok := streams.In.(*os.File)
			if !ok {
				fmt.Fprintln(streams.Err, "jumpotp: connect requires an operating-system input stream")
				return ExitMFA
			}
			source := provider.Bitwarden{}
			return runDirectWithReadiness(context.Background(), target.Manual, source, streams.Err, func() int {
				result := terminalproxy.Connect(context.Background(), terminalproxy.Options{
					Target: target,
					Source: source,
					In:     input,
					Out:    streams.Out,
					Err:    streams.Err,
				})
				if result.Err != nil && result.ExitCode != 0 {
					fmt.Fprintf(streams.Err, "jumpotp: %v\n", result.Err)
				}
				return result.ExitCode
			})
		},
		Workspace: func(cfg *config.Config, profile string, launcher string, manual bool, streams Streams) int {
			source := provider.Bitwarden{}
			return runWorkspaceWithReadiness(context.Background(), profile, manual, source, streams.Err, broker.Active, func() int {
				socket, err := broker.SocketPath(profile)
				if err != nil {
					fmt.Fprintf(streams.Err, "jumpotp: workspace broker path: %v\n", err)
					return ExitWorkspace
				}
				var server *broker.Server
				server, err = broker.NewServer(cfg, profile, source, 150*time.Millisecond)
				ownsBroker := err == nil
				if err != nil && !errors.Is(err, broker.ErrAlreadyRunning) {
					fmt.Fprintf(streams.Err, "jumpotp: start workspace broker: %v\n", err)
					return ExitWorkspace
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if ownsBroker {
					go func() {
						if serveErr := server.Serve(ctx); serveErr != nil {
							fmt.Fprintf(streams.Err, "jumpotp: workspace broker: %v\n", serveErr)
						}
					}()
					defer server.Close()
					socket = server.Socket()
				}
				manager := workspace.Manager{Config: cfg, Notice: streams.Err}
				if err := manager.Ensure(ctx, profile, launcher, manual, socket); err != nil {
					fmt.Fprintf(streams.Err, "jumpotp: prepare workspace: %v\n", err)
					return ExitWorkspace
				}
				attachStreams := workspace.AttachStreams{In: streams.In, Out: streams.Out, Err: streams.Err}
				if input, ok := streams.In.(*os.File); ok {
					attachStreams.TTY = input.Name()
				}
				if err := manager.Attach(ctx, profile, attachStreams); err != nil {
					fmt.Fprintf(streams.Err, "jumpotp: attach workspace: %v\n", err)
					return ExitWorkspace
				}
				return ExitOK
			})
		},
		Status: func(cfg *config.Config, profile string, jsonMode bool, streams Streams) int {
			report, err := (workspace.Manager{Config: cfg}).Status(context.Background(), profile)
			if err != nil {
				fmt.Fprintf(streams.Err, "jumpotp: workspace status: %v\n", err)
				return ExitWorkspace
			}
			if jsonMode {
				return writeJSON(streams.Out, report, streams.Err)
			}
			if len(report.Workspaces) == 0 {
				fmt.Fprintln(streams.Out, "No JumpOTP workspaces.")
				return ExitOK
			}
			for _, current := range report.Workspaces {
				fmt.Fprintf(streams.Out, "%s\t%s\tclients=%d\tbroker=%s\thealth=%s\n", current.Profile, current.State, current.AttachedClients, current.Broker, current.Health)
				for _, target := range current.Targets {
					fmt.Fprintf(streams.Out, "  %s\t%s\tcontrol-master=%s\n", target.Target, target.State, target.ControlMaster)
				}
			}
			return ExitOK
		},
		Stop: func(cfg *config.Config, profile string, streams Streams) int {
			stopped, err := (workspace.Manager{Config: cfg}).Stop(context.Background(), profile)
			if err != nil {
				fmt.Fprintf(streams.Err, "jumpotp: stop workspace: %v\n", err)
				return ExitWorkspace
			}
			if stopped {
				fmt.Fprintf(streams.Out, "Stopped workspace %s. OpenSSH controls any remaining ControlMaster lifetime.\n", profile)
			} else {
				fmt.Fprintf(streams.Out, "Workspace %s is already stopped.\n", profile)
			}
			return ExitOK
		},
		Doctor: func(cfg *config.Config, profile string, jsonMode bool, streams Streams) int {
			report := doctor.Run(context.Background(), cfg, profile, doctor.ExecRunner{})
			if jsonMode {
				if code := writeJSON(streams.Out, report, streams.Err); code != ExitOK {
					return code
				}
			} else {
				for _, check := range report.Checks {
					state := "ok"
					if !check.OK {
						state = "failed"
					}
					fmt.Fprintf(streams.Out, "%s\t%s\t%s\t%s\n", state, check.Name, check.Target, check.Detail)
				}
			}
			if !report.OK {
				return ExitDependency
			}
			return ExitOK
		},
	}
}
