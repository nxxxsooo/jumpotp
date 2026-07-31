package mfa

import (
	"fmt"
	"regexp"

	"github.com/nxxxsooo/jumpotp/internal/config"
)

var numericCode = regexp.MustCompile(`^[0-9]+$`)

func ValidateCode(value []byte, settings config.MFA) error {
	if !numericCode.Match(value) {
		return fmt.Errorf("code must contain only digits")
	}
	minimum, maximum := 5, 10
	if settings.Preset == "jumpserver-koko" {
		minimum, maximum = 6, 6
	}
	if settings.Digits != nil {
		minimum, maximum = *settings.Digits, *settings.Digits
	}
	if len(value) < minimum || len(value) > maximum {
		if minimum == maximum {
			return fmt.Errorf("code must contain exactly %d digits", minimum)
		}
		return fmt.Errorf("code must contain from %d through %d digits", minimum, maximum)
	}
	return nil
}

func Zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
