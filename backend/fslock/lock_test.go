package fslock

import (
	"os"
	"testing"
)

func TestLockUnlock(t *testing.T) {
	tmp, err := os.CreateTemp("", "fslock-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if err := Lock(tmp); err != nil {
		t.Fatalf("Failed to lock: %v", err)
	}

	if err := Unlock(tmp); err != nil {
		t.Fatalf("Failed to unlock: %v", err)
	}
}
