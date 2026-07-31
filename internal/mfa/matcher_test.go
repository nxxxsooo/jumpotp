package mfa

import (
	"strings"
	"testing"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

func TestStrictPresets(t *testing.T) {
	jump, err := NewMatcher(config.MFA{Preset: "jumpserver-koko"})
	if err != nil {
		t.Fatal(err)
	}
	if !jump.Feed([]byte("\x1b[32m[MFA auth]\x1b[0m: [OTP Code]: ")) {
		t.Fatal("JumpServer prompt did not match")
	}
	for _, value := range []string{"Password: ", "SMS code: ", "Email OTP: ", "WebAuthn: ", "Verification code: "} {
		matcher, _ := NewMatcher(config.MFA{Preset: "jumpserver-koko"})
		if matcher.Feed([]byte(value)) {
			t.Fatalf("matched %q", value)
		}
	}

	generic, _ := NewMatcher(config.MFA{Preset: "generic-totp"})
	if generic.Feed([]byte("Enter authenticator app co")) {
		t.Fatal("partial prompt matched too early")
	}
	if !generic.Feed([]byte("de: ")) {
		t.Fatal("split generic TOTP prompt did not match")
	}
}

func TestJumpServerKoKoCurrentPromptShape(t *testing.T) {
	matcher, err := NewMatcher(config.MFA{Preset: "jumpserver-koko"})
	if err != nil {
		t.Fatal(err)
	}
	chunks := []string{
		"synthetic-user\r\r\nPlease Enter MFA Code.\r\r\n\r",
		"(synthetic-selector@example.com) [OTP Code]: ",
	}
	if matcher.Feed([]byte(chunks[0])) {
		t.Fatal("matched before the terminal OTP prompt arrived")
	}
	if !matcher.Feed([]byte(chunks[1])) {
		t.Fatal("current JumpServer KoKo prompt did not match")
	}
}

func TestCustomAndBounds(t *testing.T) {
	matcher, err := NewMatcher(config.MFA{Preset: "custom", Pattern: `Synthetic code:\s*$`})
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.Feed([]byte("Synthetic code: ")) {
		t.Fatal("custom prompt did not match")
	}
	if _, err := NewMatcher(config.MFA{Preset: "custom", Pattern: strings.Repeat("x", maxPatternBytes+1)}); err == nil {
		t.Fatal("oversized pattern accepted")
	}
}

func TestObserverBufferIsBounded(t *testing.T) {
	matcher, _ := NewMatcher(config.MFA{Preset: "custom", Pattern: `never-match`})
	matcher.Feed([]byte(strings.Repeat("x", maxObservedBytes*2)))
	if len(matcher.buffer) != maxObservedBytes {
		t.Fatalf("buffer length = %d", len(matcher.buffer))
	}
}

func TestValidateCode(t *testing.T) {
	six := 6
	tests := []struct {
		value    string
		settings config.MFA
		ok       bool
	}{
		{"123456", config.MFA{Preset: "jumpserver-koko"}, true},
		{"12345", config.MFA{Preset: "jumpserver-koko"}, false},
		{"12345678", config.MFA{Preset: "generic-totp"}, true},
		{"1234a6", config.MFA{Preset: "generic-totp"}, false},
		{"123456", config.MFA{Preset: "custom", Digits: &six}, true},
	}
	for _, test := range tests {
		err := ValidateCode([]byte(test.value), test.settings)
		if (err == nil) != test.ok {
			t.Fatalf("%q: err = %v, ok = %v", test.value, err, test.ok)
		}
	}
}
