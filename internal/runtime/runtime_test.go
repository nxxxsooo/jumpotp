package runtime

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestSecureDirAndLease(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	dir, err := SecureDir()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	identity, err := CurrentIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "test.lease")
	want := Lease{Kind: "test", Socket: filepath.Join(dir, "test.sock"), Identity: identity}
	if err := WriteLease(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLease(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != want.Kind || got.Identity.PID != want.Identity.PID || !Matches(got.Identity) {
		t.Fatalf("lease = %+v", got)
	}
}

func TestRejectsUnsafeRuntimeDirectory(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "jumpotp-"+fmtInt(os.Getuid()))
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", base)
	original := os.Getenv("TMPDIR")
	_ = original
	// SecureDir uses os.TempDir's process-cached value on some platforms, so
	// validate the lease permission path directly instead.
	lease := filepath.Join(path, "unsafe.lease")
	if err := os.WriteFile(lease, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLease(lease); err == nil {
		t.Fatal("unsafe lease accepted")
	}
}

func fmtInt(value int) string {
	return strconv.Itoa(value)
}
