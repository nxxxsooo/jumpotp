package launcher

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

func TestBuildUsesExactArguments(t *testing.T) {
	tests := []struct {
		launcher string
		want     Spec
	}{
		{"ssh", Spec{Executable: "ssh", Args: []string{"example-one"}}},
		{"sshm", Spec{Executable: "sshm", Args: []string{"example-one"}}},
	}
	for _, test := range tests {
		got, err := Build(config.EffectiveTarget{Launcher: test.launcher, SSH: "example-one"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got %+v, want %+v", test.launcher, got, test.want)
		}
	}
}

func TestBuildRejectsUnknownLauncher(t *testing.T) {
	if _, err := Build(config.EffectiveTarget{Launcher: "sh", SSH: "example-one"}); err == nil {
		t.Fatal("Build succeeded")
	}
}

func TestSSHMProcessReplacementCompatibility(t *testing.T) {
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "sshm"), "#!/bin/sh\nexec ssh \"$@\"\n")
	writeExecutable(t, filepath.Join(bin, "ssh"), "#!/bin/sh\nprintf 'ssh:%s\\n' \"$1\"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	command, err := CommandContext(context.Background(), config.EffectiveTarget{Launcher: "sshm", SSH: "example-one"})
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("CombinedOutput: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != "ssh:example-one" {
		t.Fatalf("output = %q", output)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
}
