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
		return Spec{Executable: "ssh", Args: []string{target.SSH}}, nil
	case "sshm":
		return Spec{Executable: "sshm", Args: []string{target.SSH}}, nil
	default:
		return Spec{}, fmt.Errorf("unsupported launcher %q", target.Launcher)
	}
}

func CommandContext(ctx context.Context, target config.EffectiveTarget) (*exec.Cmd, error) {
	spec, err := Build(target)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, spec.Executable, spec.Args...), nil
}
