//go:build windows

package persistenthost

import (
	"errors"

	"golang.org/x/sys/windows"
)

// sharingViolation reports the transient Windows error for a file another
// process still holds open.
func sharingViolation(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
