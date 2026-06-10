//go:build !unix

package secrets

import "os"

func verifyOwner(path string) error {
	_, err := os.Stat(path)
	return err
}
