package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	keyringService = "enough"
	keyringAccount = "opencode-api-key"
)

var ErrNotConnected = errors.New("not connected — run: enough connect")

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "enough"), nil
}

func credentialsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

// SetAPIKey stores the key in the OS secret service, falling back to a
// user-only file (0600) when no keyring is available.
func SetAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("api key cannot be empty")
	}

	if err := keyring.Set(keyringService, keyringAccount, key); err == nil {
		_ = removeFile()
		return nil
	}

	return writeFile(key)
}

// GetAPIKey returns the stored API key for the current user.
func GetAPIKey() (string, error) {
	if key, err := keyring.Get(keyringService, keyringAccount); err == nil && key != "" {
		return key, nil
	}

	key, err := readFile()
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", ErrNotConnected
	}
	return key, nil
}

// HasAPIKey reports whether a key is stored.
func HasAPIKey() bool {
	_, err := GetAPIKey()
	return err == nil
}

// DeleteAPIKey removes stored credentials.
func DeleteAPIKey() error {
	_ = keyring.Delete(keyringService, keyringAccount)
	return removeFile()
}

func writeFile(key string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	path, err := credentialsPath()
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func readFile() (string, error) {
	path, err := credentialsPath()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotConnected
	}
	if err != nil {
		return "", err
	}

	if err := verifyOwner(path); err != nil {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func removeFile() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
