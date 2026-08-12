package launcher

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

type Spec struct {
	Executable string
	Args       []string
}

func Build(target config.EffectiveTarget) (Spec, error) {
	switch target.Launcher {
	case "ssh":
		return Spec{Executable: "ssh", Args: connectArgs(target)}, nil
	case "sshm":
		return Spec{Executable: "sshm", Args: connectArgs(target)}, nil
	default:
		return Spec{}, fmt.Errorf("unsupported launcher %q", target.Launcher)
	}
}

func connectArgs(target config.EffectiveTarget) []string {
	if target.Master {
		return []string{"-N", "-o", "ServerAliveInterval=60", "-o", "ServerAliveCountMax=3", "-o", "ControlPersist=no", target.SSH}
	}
	return []string{target.SSH}
}

func CommandContext(ctx context.Context, target config.EffectiveTarget) (*exec.Cmd, error) {
	spec, err := Build(target)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, spec.Executable, spec.Args...), nil
}
