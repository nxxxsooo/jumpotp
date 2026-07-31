package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
)

type Identity struct {
	PID        int    `json:"pid"`
	UID        uint32 `json:"uid"`
	Start      string `json:"start"`
	Executable string `json:"executable"`
}

type Lease struct {
	Kind     string   `json:"kind"`
	Profile  string   `json:"profile,omitempty"`
	Socket   string   `json:"socket"`
	Identity Identity `json:"identity"`
}

func SecureDir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	path := filepath.Join(base, "jumpotp-"+strconv.Itoa(os.Getuid()))
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		path = filepath.Join(base, "jumpotp")
	}
	if len(path) > 80 {
		path = filepath.Join(os.TempDir(), "jumpotp-"+strconv.Itoa(os.Getuid()))
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return "", fmt.Errorf("create runtime directory: %w", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return "", fmt.Errorf("inspect runtime directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("runtime path must be a real directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return "", errors.New("runtime directory must be owned by the current user")
	}
	if info.Mode().Perm() != 0o700 {
		return "", errors.New("runtime directory permissions must be 0700")
	}
	return path, nil
}

func CurrentIdentity() (Identity, error) {
	return ProcessIdentity(os.Getpid())
}

func WriteLease(path string, lease Lease) error {
	data, err := json.Marshal(lease)
	if err != nil {
		return fmt.Errorf("encode runtime lease: %w", err)
	}
	dir := filepath.Dir(path)
	temp, err := os.OpenFile(filepath.Join(dir, "."+filepath.Base(path)+".tmp-"+strconv.Itoa(os.Getpid())), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create runtime lease: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write runtime lease: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync runtime lease: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close runtime lease: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("install runtime lease: %w", err)
	}
	return nil
}

func ReadLease(path string) (Lease, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Lease{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Lease{}, errors.New("runtime lease must be a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return Lease{}, errors.New("runtime lease must be owned by the current user")
	}
	if info.Mode().Perm() != 0o600 {
		return Lease{}, errors.New("runtime lease permissions must be 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Lease{}, err
	}
	var lease Lease
	if err := json.Unmarshal(data, &lease); err != nil {
		return Lease{}, fmt.Errorf("decode runtime lease: %w", err)
	}
	return lease, nil
}

func Matches(identity Identity) bool {
	current, err := ProcessIdentity(identity.PID)
	if err != nil {
		return false
	}
	return current.UID == identity.UID &&
		current.Start == identity.Start &&
		executableMatches(current.Executable, identity.Executable)
}

func executableMatches(current, leased string) bool {
	if runtime.GOOS == "darwin" {
		return filepath.Base(current) == filepath.Base(leased)
	}
	currentPath, currentErr := filepath.EvalSymlinks(current)
	leasedPath, leasedErr := filepath.EvalSymlinks(leased)
	if currentErr == nil {
		current = currentPath
	}
	if leasedErr == nil {
		leased = leasedPath
	}
	return current == leased
}
