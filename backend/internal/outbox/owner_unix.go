//go:build !windows

package outbox

import (
	"fmt"
	"os"
	"syscall"
)

func checkOwner(dir string, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("outbox %s is accessible to other users (mode %o)", dir, info.Mode().Perm())
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("outbox %s belongs to uid %d, not %d", dir, st.Uid, os.Getuid())
	}
	return nil
}
