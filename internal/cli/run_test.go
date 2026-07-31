package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

func TestVersionJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run([]string{"version", "--json"}, strings.NewReader(""), &out, &errOut)
	if code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["schema_version"] != float64(1) || result["name"] != "jumpotp" {
		t.Fatalf("result = %#v", result)
	}
}

func TestConnectOptionParsingAndInheritance(t *testing.T) {
	path := writeTestConfig(t)
	var got config.EffectiveTarget
	handlers := noOpHandlers()
	handlers.Connect = func(_ *config.Config, target config.EffectiveTarget, _ Streams) int {
		got = target
		return ExitOK
	}
	var out, errOut bytes.Buffer
	code := RunWithHandlers(
		[]string{"--config", path, "connect", "production/app-01", "--launcher", "sshm", "--manual"},
		Streams{In: strings.NewReader(""), Out: &out, Err: &errOut},
		handlers,
	)
	if code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	if got.Launcher != "sshm" || !got.Manual || got.Item != "Example Login" || got.SSH != "production-app-01" {
		t.Fatalf("effective target = %+v", got)
	}
}

func TestConfigValidateJSON(t *testing.T) {
	path := writeTestConfig(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"config", "validate", path, "--json"}, strings.NewReader(""), &out, &errOut)
	if code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	var result struct {
		SchemaVersion int              `json:"schema_version"`
		Valid         bool             `json:"valid"`
		Errors        []config.Problem `json:"errors"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != 1 || !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestConfigPathConflict(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Run(
		[]string{"--config", "first.yaml", "config", "validate", "second.yaml"},
		strings.NewReader(""), &out, &errOut,
	)
	if code != ExitUsage || !strings.Contains(errOut.String(), "conflicts") {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
}

func TestConfigInitAndForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var out, errOut bytes.Buffer
	if code := Run([]string{"--config", path, "config", "init"}, strings.NewReader(""), &out, &errOut); code != ExitOK {
		t.Fatalf("code = %d, stderr = %s", code, errOut.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte(config.Sample)) {
		t.Fatal("generated sample differs from config.Sample")
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"--config", path, "config", "init"}, strings.NewReader(""), &out, &errOut); code != ExitUsage {
		t.Fatalf("second code = %d", code)
	}
	if code := Run([]string{"--config", path, "config", "init", "--force"}, strings.NewReader(""), &out, &errOut); code != ExitOK {
		t.Fatalf("force code = %d, stderr = %s", code, errOut.String())
	}
}

func TestUsageErrors(t *testing.T) {
	tests := [][]string{
		{},
		{"unknown"},
		{"connect"},
		{"connect", "production/app-01", "remote-command"},
		{"status", "one", "two"},
		{"stop"},
		{"doctor", "--bad"},
		{"version", "extra"},
		{"help", "unknown"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := Run(args, strings.NewReader(""), &out, &errOut); code != ExitUsage {
				t.Fatalf("Run(%q) code = %d, stderr = %s", args, code, errOut.String())
			}
		})
	}
}

func TestStatusJSONWithNoWorkspaceServer(t *testing.T) {
	runtimeBase, err := os.MkdirTemp("/tmp", "jotp-cli-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runtimeBase)
	t.Setenv("XDG_RUNTIME_DIR", runtimeBase)
	path := writeTestConfig(t)
	var out, errOut bytes.Buffer
	code := Run([]string{"--config", path, "status", "--json"}, strings.NewReader(""), &out, &errOut)
	if code != ExitOK {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
	if out.String() != "{\"schema_version\":1,\"workspaces\":[]}\n" {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestInternalCommandsRequireGuardEnvironment(t *testing.T) {
	t.Setenv("JUMPOTP_INTERNAL", "")
	var out, errOut bytes.Buffer
	code := Run([]string{"__target"}, strings.NewReader(""), &out, &errOut)
	if code != ExitUsage || !strings.Contains(errOut.String(), "rejected") {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(config.Sample), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func noOpHandlers() Handlers {
	return Handlers{
		Connect: func(*config.Config, config.EffectiveTarget, Streams) int { return ExitOK },
		Workspace: func(*config.Config, string, string, bool, Streams) int {
			return ExitOK
		},
		Status: func(*config.Config, string, bool, Streams) int { return ExitOK },
		Stop:   func(*config.Config, string, Streams) int { return ExitOK },
		Doctor: func(*config.Config, string, bool, Streams) int { return ExitOK },
	}
}
