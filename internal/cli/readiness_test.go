package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nxxxsooo/jumpotp/internal/provider"
)

type readinessRecorder struct {
	events *[]string
	err    error
	calls  int
}

func (r *readinessRecorder) Ready(context.Context) error {
	r.calls++
	if r.events != nil {
		*r.events = append(*r.events, "ready")
	}
	return r.err
}

func TestDirectReadinessRunsBeforeLaunch(t *testing.T) {
	events := []string{}
	source := &readinessRecorder{events: &events}
	var errOut bytes.Buffer
	code := runDirectWithReadiness(context.Background(), false, source, &errOut, func() int {
		events = append(events, "launch")
		return ExitOK
	})
	if code != ExitOK || strings.Join(events, ",") != "ready,launch" {
		t.Fatalf("code = %d, events = %#v", code, events)
	}
	if errOut.Len() != 0 {
		t.Fatalf("successful readiness emitted diagnostics: %q", errOut.String())
	}
}

func TestReadinessFailureWarnsAndContinuesWithoutPrivateDetail(t *testing.T) {
	var errOut bytes.Buffer
	source := &readinessRecorder{err: errors.New("private provider detail")}
	launched := false
	code := runDirectWithReadiness(context.Background(), false, source, &errOut, func() int {
		launched = true
		return ExitOK
	})
	if code != ExitOK || !launched {
		t.Fatalf("code = %d, launched = %v", code, launched)
	}
	if got := errOut.String(); got != "jumpotp: Bitwarden readiness check failed; continuing with configured MFA fallback\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestMeasuredReadinessFailureWarnsWithBoundedTiming(t *testing.T) {
	var errOut bytes.Buffer
	source := &readinessRecorder{err: provider.NewMeasuredError(provider.TimedOut, 6)}
	code := runDirectWithReadiness(context.Background(), false, source, &errOut, func() int { return ExitOK })
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	if got := errOut.String(); got != "jumpotp: Bitwarden readiness check timed out after 6s; continuing with configured MFA fallback\n" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestManualDirectSkipsReadiness(t *testing.T) {
	source := &readinessRecorder{}
	code := runDirectWithReadiness(context.Background(), true, source, &bytes.Buffer{}, func() int { return ExitOK })
	if code != ExitOK || source.calls != 0 {
		t.Fatalf("code = %d, readiness calls = %d", code, source.calls)
	}
}

func TestNewWorkspaceReadinessRunsBeforeTargetCreation(t *testing.T) {
	events := []string{}
	source := &readinessRecorder{events: &events}
	code := runWorkspaceWithReadiness(context.Background(), "production", false, source, &bytes.Buffer{},
		func(string) (bool, error) { return false, nil },
		func() int {
			events = append(events, "create-targets")
			return ExitOK
		})
	if code != ExitOK || strings.Join(events, ",") != "ready,create-targets" {
		t.Fatalf("code = %d, events = %#v", code, events)
	}
}

func TestActiveBrokerReattachmentSkipsReadiness(t *testing.T) {
	source := &readinessRecorder{}
	code := runWorkspaceWithReadiness(context.Background(), "production", false, source, &bytes.Buffer{},
		func(string) (bool, error) { return true, nil },
		func() int { return ExitOK })
	if code != ExitOK || source.calls != 0 {
		t.Fatalf("code = %d, readiness calls = %d", code, source.calls)
	}
}

func TestAmbiguousBrokerProbeDefersToLifecycleWithoutReadiness(t *testing.T) {
	source := &readinessRecorder{}
	code := runWorkspaceWithReadiness(context.Background(), "production", false, source, &bytes.Buffer{},
		func(string) (bool, error) { return false, errors.New("conflicting broker state") },
		func() int { return ExitWorkspace })
	if code != ExitWorkspace || source.calls != 0 {
		t.Fatalf("code = %d, readiness calls = %d", code, source.calls)
	}
}

func TestManualWorkspaceSkipsProbeAndReadiness(t *testing.T) {
	source := &readinessRecorder{}
	probeCalls := 0
	code := runWorkspaceWithReadiness(context.Background(), "production", true, source, &bytes.Buffer{},
		func(string) (bool, error) {
			probeCalls++
			return false, nil
		},
		func() int { return ExitOK })
	if code != ExitOK || source.calls != 0 || probeCalls != 0 {
		t.Fatalf("code = %d, readiness calls = %d, probe calls = %d", code, source.calls, probeCalls)
	}
}
