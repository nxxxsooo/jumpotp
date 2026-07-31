//go:build linux

package runtime

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func ProcessIdentity(pid int) (Identity, error) {
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return Identity{}, err
	}
	stat := string(statData)
	closeIndex := strings.LastIndex(stat, ")")
	if closeIndex < 0 {
		return Identity{}, fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(stat[closeIndex+1:])
	if len(fields) < 20 {
		return Identity{}, fmt.Errorf("incomplete process stat")
	}
	start := fields[19]
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return Identity{}, err
	}
	var uid uint64
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				uid, err = strconv.ParseUint(parts[1], 10, 32)
			}
			break
		}
	}
	if err != nil {
		return Identity{}, err
	}
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return Identity{}, err
	}
	return Identity{PID: pid, UID: uint32(uid), Start: start, Executable: executable}, nil
}
