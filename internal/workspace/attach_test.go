package workspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

type attachRunner struct {
	mu        sync.Mutex
	listCalls int
	calls     []string
}

func (r *attachRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if containsArg(args, "switch-client") {
		return nil, nil
	}
	if containsArg(args, "list-clients") {
		r.listCalls++
		if r.listCalls == 1 {
			return []byte("/dev/ttys999\tproduction\n"), nil
		}
		return []byte("/dev/ttys999\tother\n"), nil
	}
	return nil, errors.New("unexpected command")
}

func (r *attachRunner) Interactive(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error {
	return errors.New("unexpected interactive call")
}

func (r *attachRunner) LookPath(name string) (string, error) {
	return "/synthetic/bin/" + name, nil
}

func TestAttachRefusesUnrelatedTmux(t *testing.T) {
	useShortAttachRuntime(t)
	cfg, err := config.Parse([]byte(config.Sample))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", "/private/tmp/unrelated.sock,1,0")
	err = (Manager{Config: cfg, Runner: &attachRunner{}}).Attach(
		context.Background(),
		"production",
		AttachStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}},
	)
	if err == nil || !strings.Contains(err.Error(), "refusing nested") {
		t.Fatalf("error = %v", err)
	}
}

func TestSameServerSwitchKeepsBrokerOwnerUntilClientLeaves(t *testing.T) {
	useShortAttachRuntime(t)
	cfg, err := config.Parse([]byte(config.Sample))
	if err != nil {
		t.Fatal(err)
	}
	manager := Manager{Config: cfg, Runner: &attachRunner{}}
	socket, _, err := manager.Paths()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX", socket+",123,0")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = manager.Attach(ctx, "production", AttachStreams{
		In:  strings.NewReader(""),
		Out: &bytes.Buffer{},
		Err: &bytes.Buffer{},
		TTY: "/dev/ttys999",
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := manager.Runner.(*attachRunner)
	if runner.listCalls < 2 {
		t.Fatalf("list client calls = %d", runner.listCalls)
	}
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}

func useShortAttachRuntime(t *testing.T) {
	t.Helper()
	base, err := os.MkdirTemp("/private/tmp", "jotp-attach-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	t.Setenv("XDG_RUNTIME_DIR", base)
}
