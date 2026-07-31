package mfa

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

const (
	maxObservedBytes = 16 * 1024
	maxPatternBytes  = 4096
)

var (
	ansiCSI = regexp.MustCompile("\\x1b\\[[0-?]*[ -/]*[@-~]")
	ansiOSC = regexp.MustCompile("\\x1b\\][^\\x07]*(?:\\x07|\\x1b\\\\)")
)

type Matcher struct {
	expression *regexp.Regexp
	buffer     string
}

func NewMatcher(settings config.MFA) (*Matcher, error) {
	var pattern string
	switch settings.Preset {
	case "jumpserver-koko":
		pattern = `(?i)(?:\[MFA auth\]\s*:\s*\[OTP Code\]|please\s+enter\s+mfa\s+code\.?[\s\S]{0,512}\[OTP Code\]|(?:please\s+)?enter\s+(?:the\s+)?6[- ]digit(?:s)?(?:\s+(?:verification|authenticator|MFA))?\s+code)\s*[:：]?\s*$`
	case "generic-totp":
		pattern = `(?i)(?:TOTP|time[- ]based one[- ]time password|authenticator(?: app)?)(?:[^\r\n]{0,64})(?:code|token)\s*[:：]\s*$`
	case "custom":
		if settings.Pattern == "" {
			return nil, fmt.Errorf("custom matcher pattern is required")
		}
		if len([]byte(settings.Pattern)) > maxPatternBytes {
			return nil, fmt.Errorf("custom matcher pattern exceeds %d bytes", maxPatternBytes)
		}
		pattern = settings.Pattern
	default:
		return nil, fmt.Errorf("unsupported MFA preset %q", settings.Preset)
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile MFA matcher: %w", err)
	}
	return &Matcher{expression: expression}, nil
}

func (m *Matcher) Feed(value []byte) bool {
	normalized := normalize(value)
	m.buffer += normalized
	if len(m.buffer) > maxObservedBytes {
		m.buffer = m.buffer[len(m.buffer)-maxObservedBytes:]
	}
	return m.expression.MatchString(m.buffer)
}

func (m *Matcher) Reset() {
	m.buffer = ""
}

func normalize(value []byte) string {
	text := string(value)
	text = ansiOSC.ReplaceAllString(text, "")
	text = ansiCSI.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}
