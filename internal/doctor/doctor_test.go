package doctor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

type fakeRunner struct {
	missing map[string]bool
	calls   []string
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	f.calls = append(f.calls, "look:"+name)
	if f.missing[name] {
		return "", errors.New("missing")
	}
	return "/synthetic/bin/" + name, nil
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, "run:"+name+" "+strings.Join(args, " "))
	if f.missing[name] {
		return nil, errors.New("missing")
	}
	return []byte(name + " synthetic-version\n"), nil
}

func TestDoctorIsNonConnectingAndDeterministic(t *testing.T) {
	cfg, err := config.Parse([]byte(config.Sample))
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{missing: map[string]bool{}}
	report := Run(context.Background(), cfg, "production", runner)
	if !report.OK {
		t.Fatalf("report = %+v", report)
	}
	for _, call := range runner.calls {
		for _, forbidden := range []string{"get totp", "login", "unlock", "new-session", "connect"} {
			if strings.Contains(call, forbidden) {
				t.Fatalf("forbidden call %q", call)
			}
		}
	}
	wantTail := []string{
		"run:ssh -G production-app-01",
		"run:ssh -G production-app-02",
	}
	if !reflect.DeepEqual(runner.calls[len(runner.calls)-2:], wantTail) {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestDoctorReportsMissingDependency(t *testing.T) {
	cfg, err := config.Parse([]byte(config.Sample))
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{missing: map[string]bool{"bw": true}}
	report := Run(context.Background(), cfg, "", runner)
	if report.OK {
		t.Fatalf("report = %+v", report)
	}
}
