package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/broker"
	"github.com/nxxxsooo/jumpotp/internal/config"
	"github.com/nxxxsooo/jumpotp/internal/probes"
	"github.com/nxxxsooo/jumpotp/internal/supervise"
	terminalproxy "github.com/nxxxsooo/jumpotp/internal/terminal"
)

// masterCheckTimeout bounds the "ssh -O check <alias>" gate probe, mirroring
// internal/workspace/status.go's controlMasterState.
const masterCheckTimeout = 3 * time.Second

func runInternal(args []string, streams Streams) int {
	if os.Getenv("JUMPOTP_INTERNAL") != "1" || len(args) == 0 {
		return usageError(streams.Err, errors.New("internal command rejected"))
	}
	switch args[0] {
	case "__target":
		values, flags, err := parseInternalFlags(args[1:], map[string]bool{
			"--config": true, "--profile": true, "--target": true, "--broker": true, "--launcher": true, "--manual": false,
		})
		if err != nil || len(values) != 0 {
			if err == nil {
				err = errors.New("internal target accepts flags only")
			}
			return usageError(streams.Err, err)
		}
		for _, required := range []string{"--config", "--profile", "--target", "--broker"} {
			if flags[required] == "" {
				return usageError(streams.Err, fmt.Errorf("%s is required", required))
			}
		}
		cfg, err := config.Load(flags["--config"])
		if err != nil {
			return configError(streams.Err, err)
		}
		target, err := cfg.Resolve(flags["--profile"], flags["--target"], flags["--launcher"], flags["--manual"] == "true")
		if err != nil {
			return usageError(streams.Err, err)
		}
		target.Master = true
		input, ok := streams.In.(*os.File)
		if !ok {
			fmt.Fprintln(streams.Err, "jumpotp: target wrapper requires an operating-system input stream")
			return ExitMFA
		}
		brokerSocket := flags["--broker"]
		sup := supervise.Supervisor{
			Profile: target.Profile,
			Target:  target.Target,
			Connect: func(ctx context.Context) supervise.ConnectResult {
				source := &broker.Client{Socket: brokerSocket, Profile: target.Profile, Target: target.Target}
				result := terminalproxy.Connect(ctx, terminalproxy.Options{
					Target: target,
					Source: source,
					In:     input,
					Out:    streams.Out,
					Err:    streams.Err,
				})
				return supervise.ConnectResult{ExitCode: result.ExitCode, Err: result.Err}
			},
			MasterCheck: func() (bool, error) {
				checkCtx, cancel := context.WithTimeout(context.Background(), masterCheckTimeout)
				defer cancel()
				_, err := exec.CommandContext(checkCtx, "ssh", "-O", "check", target.SSH).Output()
				return err == nil, nil
			},
			BrokerActive: func() (bool, error) {
				return broker.Active(target.Profile)
			},
			Out: streams.Err,
		}
		result := sup.Run(context.Background())
		if result.Err != nil && result.ExitCode != 0 {
			fmt.Fprintf(streams.Err, "jumpotp: %v\n", result.Err)
		}
		return result.ExitCode
	case "__health":
		values, flags, err := parseInternalFlags(args[1:], map[string]bool{"--config": true, "--profile": true})
		if err != nil || len(values) != 0 || flags["--config"] == "" || flags["--profile"] == "" {
			if err == nil {
				err = errors.New("internal health requires --config and --profile")
			}
			return usageError(streams.Err, err)
		}
		cfg, err := config.Load(flags["--config"])
		if err != nil {
			return configError(streams.Err, err)
		}
		if err := (probes.Scheduler{Config: cfg, Profile: flags["--profile"], Out: streams.Out}).Run(context.Background()); err != nil {
			fmt.Fprintf(streams.Err, "jumpotp: health scheduler: %v\n", err)
			return ExitWorkspace
		}
		return ExitOK
	default:
		return usageError(streams.Err, errors.New("internal command rejected"))
	}
}

func parseInternalFlags(args []string, allowed map[string]bool) ([]string, map[string]string, error) {
	values := []string{}
	flags := map[string]string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		expectsValue, ok := allowed[arg]
		if !ok {
			if strings.HasPrefix(arg, "-") {
				return nil, nil, fmt.Errorf("unknown internal flag %q", arg)
			}
			values = append(values, arg)
			continue
		}
		if _, exists := flags[arg]; exists {
			return nil, nil, fmt.Errorf("%s may be supplied only once", arg)
		}
		if expectsValue {
			if index+1 >= len(args) {
				return nil, nil, fmt.Errorf("%s requires a value", arg)
			}
			flags[arg] = args[index+1]
			index++
		} else {
			flags[arg] = "true"
		}
	}
	return values, flags, nil
}
