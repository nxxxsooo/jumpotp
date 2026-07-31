//go:build darwin

package runtime

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProcessIdentityClassifiesExitedPIDAsAbsent(t *testing.T) {
	const pid = 99999
	if err := unix.Kill(pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Skipf("synthetic PID is not absent: %v", err)
	}
	if _, err := ProcessIdentity(pid); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ProcessIdentity error = %v, want os.ErrNotExist", err)
	}
}
