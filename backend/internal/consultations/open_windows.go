package consultations

import "os"

func openNonblocking(root *os.Root, path string) (*os.File, error) {
	return root.Open(path)
}
