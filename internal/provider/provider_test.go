package provider

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	args   []string
	stdout string
	stderr string
	err    error
	block  bool
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdout, stderr io.Writer) error {
	f.args = append([]string{name}, args...)
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	_, _ = io.WriteString(stdout, f.stdout)
	_, _ = io.WriteString(stderr, f.stderr)
	return f.err
}

func TestBitwardenUsesExactArguments(t *testing.T) {
	runner := &fakeRunner{stdout: "123456\n"}
	code, err := (Bitwarden{Runner: runner}).Code(context.Background(), "Example Login")
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(code)
	if string(code) != "123456" {
		t.Fatalf("code = %q", code)
	}
	want := []string{"bw", "get", "totp", "Example Login"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("args = %#v, want %#v", runner.args, want)
	}
}

func TestProviderClassification(t *testing.T) {
	tests := []struct {
		message string
		kind    Kind
	}{
		{"Vault is locked.", Locked},
		{"More than one result was found.", Ambiguous},
		{"Item not found.", Missing},
		{"Unexpected failure.", Failed},
	}
	for _, test := range tests {
		runner := &fakeRunner{stderr: test.message, err: errors.New("exit 1")}
		_, err := (Bitwarden{Runner: runner}).Code(context.Background(), "Synthetic")
		if KindOf(err) != test.kind {
			t.Fatalf("%q: got %q", test.message, KindOf(err))
		}
		if strings.Contains(SafeMessage(err), "Synthetic") {
			t.Fatal("safe message contains item")
		}
	}
}

func TestProviderTimeout(t *testing.T) {
	runner := &fakeRunner{block: true}
	_, err := (Bitwarden{Runner: runner, Timeout: time.Millisecond}).Code(context.Background(), "Synthetic")
	if KindOf(err) != TimedOut {
		t.Fatalf("kind = %q, err = %v", KindOf(err), err)
	}
}

func TestProviderRejectsOversizedOutput(t *testing.T) {
	runner := &fakeRunner{stdout: strings.Repeat("1", maxCodeOutput+1)}
	_, err := (Bitwarden{Runner: runner}).Code(context.Background(), "Synthetic")
	if KindOf(err) != Invalid {
		t.Fatalf("kind = %q, err = %v", KindOf(err), err)
	}
}
