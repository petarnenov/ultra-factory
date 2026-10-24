//go:build !windows

package consultations

import (
	"os"

	"golang.org/x/sys/unix"
)

func openNonblocking(root *os.Root, path string) (*os.File, error) {
	return root.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
