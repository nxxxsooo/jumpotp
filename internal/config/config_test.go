package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSampleParsesAndResolves(t *testing.T) {
	cfg, err := Parse([]byte(Sample))
	if err != nil {
		t.Fatalf("Parse(Sample): %v", err)
	}
	target, err := cfg.Resolve("production", "app-02", "", false)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if target.Launcher != "ssh" || target.Provider != "bitwarden" ||
		target.Item != "Example Login Override" || target.Fallback != "prompt" {
		t.Fatalf("unexpected effective target: %+v", target)
	}
	if !target.Health.Enabled && target.Health.Interval != 4*time.Minute {
		t.Fatalf("unexpected health defaults: %+v", target.Health)
	}
	if got, want := len(target.Health.Probes), 6; got != want {
		t.Fatalf("probe count = %d, want %d", got, want)
	}
}

func TestDuplicateKeyRejected(t *testing.T) {
	_, err := Parse([]byte(`version: 1
profiles:
  demo:
    mfa: {preset: generic-totp}
    otp: {item: Example Login}
    targets: {one: {ssh: example-one}}
    targets: {two: {ssh: example-two}}
`))
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
	if len(validation.Problems) != 1 || validation.Problems[0].Code != "duplicate_key" {
		t.Fatalf("problems = %+v", validation.Problems)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	_, err := Parse([]byte(`version: 1
profiles:
  demo:
    launcer: ssh
    mfa: {preset: generic-totp}
    otp: {item: Example Login}
    targets: {one: {ssh: example-one}}
`))
	if err == nil || !strings.Contains(err.Error(), "launcer") {
		t.Fatalf("error = %v, want unknown field", err)
	}
}

func TestValidationProblemsAreSorted(t *testing.T) {
	cfg := &Config{
		Version: 2,
		Defaults: Defaults{
			Launcher: "bad",
			OTP:      OTPDefaults{Provider: "other", Fallback: "hide"},
		},
		Profiles: map[string]Profile{},
	}
	problems := cfg.Validate()
	paths := make([]string, len(problems))
	for index := range problems {
		paths[index] = problems[index].Path
	}
	want := []string{"defaults.launcher", "defaults.otp.fallback", "defaults.otp.provider", "profiles", "version"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestCrossReferenceAndBounds(t *testing.T) {
	cfg, err := Parse([]byte(`version: 1
probes:
  - id: disabled
    enabled: false
    label: Disabled
    platform: any
    command: "true"
profiles:
  demo:
    mfa:
      preset: custom
      pattern: "Authenticator code:"
      digits: 8
    otp:
      item: Example Login With Spaces
    targets:
      one:
        ssh: example-one
    workspace:
      health:
        enabled: true
        interval: 10s
        commands: [disabled, missing]
`))
	if err == nil {
		t.Fatal("Parse succeeded, want validation failure")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v", err, err)
	}
	codes := map[string]bool{}
	for _, problem := range validation.Problems {
		codes[problem.Code] = true
	}
	for _, code := range []string{"invalid_interval", "disabled_probe", "unknown_probe"} {
		if !codes[code] {
			t.Fatalf("missing code %q in %+v", code, validation.Problems)
		}
	}
	_ = cfg
}

func TestUnsupportedYAMLFeaturesRejected(t *testing.T) {
	tests := map[string]string{
		"alias":  "version: 1\nprofiles: &profiles {}\ncopy: *profiles\n",
		"merge":  "version: 1\ndefaults: &d {launcher: ssh}\nprofiles:\n  demo:\n    <<: *d\n",
		"tag":    "version: 1\nprofiles: !private {}\n",
		"second": "version: 1\nprofiles: {}\n---\nversion: 1\nprofiles: {}\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(input)); err == nil {
				t.Fatal("Parse succeeded")
			}
		})
	}
}

func TestDiscover(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/example-xdg")
	got, err := Discover("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/example-xdg/jumpotp/config.yaml" {
		t.Fatalf("got %q", got)
	}
	explicit, err := Discover("relative.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(explicit) {
		t.Fatalf("explicit path is not absolute: %q", explicit)
	}
}

func TestWriteSampleProtectsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	if err := WriteSample(path, false); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	if err := WriteSample(path, false); err == nil {
		t.Fatal("second WriteSample succeeded")
	}
	if err := WriteSample(path, true); err != nil {
		t.Fatalf("forced WriteSample: %v", err)
	}
}

func TestParseAddress(t *testing.T) {
	profile, target, err := ParseAddress("production/app-01")
	if err != nil || profile != "production" || target != "app-01" {
		t.Fatalf("got %q %q %v", profile, target, err)
	}
	for _, value := range []string{"missing", "too/many/parts", "bad name/target", "profile/bad@target"} {
		if _, _, err := ParseAddress(value); err == nil {
			t.Fatalf("ParseAddress(%q) succeeded", value)
		}
	}
}
