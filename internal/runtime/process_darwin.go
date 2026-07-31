//go:build darwin

package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func ProcessIdentity(pid int) (Identity, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		// macOS reports EIO rather than ESRCH when kern.proc.pid races with a
		// process exit. Only classify it as absent after kill(2) independently
		// confirms that the PID no longer exists.
		if errors.Is(err, unix.EIO) && errors.Is(unix.Kill(pid, 0), unix.ESRCH) {
			return Identity{}, os.ErrNotExist
		}
		return Identity{}, err
	}
	if int(info.Proc.P_pid) != pid {
		return Identity{}, fmt.Errorf("process identity mismatch")
	}
	nameBytes := info.Proc.P_comm[:]
	if index := bytes.IndexByte(nameBytes, 0); index >= 0 {
		nameBytes = nameBytes[:index]
	}
	name := string(nameBytes)
	start := fmt.Sprintf("%d.%06d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec)
	return Identity{
		PID:        pid,
		UID:        info.Eproc.Ucred.Uid,
		Start:      start,
		Executable: name,
	}, nil
}
