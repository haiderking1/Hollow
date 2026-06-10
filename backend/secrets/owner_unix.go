//go:build unix

package secrets

import (
	"fmt"
	"os"
	"syscall"
)

func verifyOwner(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	if stat.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("credentials file is not owned by the current user")
	}
	return nil
}
