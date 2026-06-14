package browser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCdpPort(t *testing.T) {
	if p := parseCdpPort("http://127.0.0.1:9222"); p != 9222 {
		t.Errorf("expected 9222, got %d", p)
	}
	if p := parseCdpPort("http://127.0.0.1"); p != 9222 {
		t.Errorf("expected 9222, got %d", p)
	}
	if p := parseCdpPort("http://127.0.0.1:9333"); p != 9333 {
		t.Errorf("expected 9333, got %d", p)
	}
}

func TestIsCdpConnectionError(t *testing.T) {
	if !isCdpConnectionError(fmt.Errorf("fetch failed")) {
		t.Errorf("expected true for fetch failed")
	}
	if !isCdpConnectionError(fmt.Errorf("connect ECONNREFUSED 127.0.0.1:9222")) {
		t.Errorf("expected true for ECONNREFUSED")
	}
	if isCdpConnectionError(fmt.Errorf("bad selector")) {
		t.Errorf("expected false for bad selector")
	}
}

func TestShouldAutoLaunchBrowser(t *testing.T) {
	os.Setenv("ENOUGH_BROWSER_AUTO_LAUNCH", "0")
	defer os.Unsetenv("ENOUGH_BROWSER_AUTO_LAUNCH")
	if ShouldAutoLaunchBrowser() {
		t.Errorf("expected false when ENOUGH_BROWSER_AUTO_LAUNCH=0")
	}
}

func TestWaitForCdpReady(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			http.Error(w, "not ready", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"Browser": "Chrome"}`))
	}))
	defer server.Close()

	ready, err := waitForCdpReady(server.URL, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Errorf("expected ready to be true")
	}
	if attempts < 3 {
		t.Errorf("expected at least 3 attempts, got %d", attempts)
	}
}

func TestResolveBrowserExecutable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "enough-browser-exe-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	exePath := filepath.Join(tmpDir, "mock-chrome")
	if err := os.WriteFile(exePath, []byte(""), 0700); err != nil {
		t.Fatalf("failed to write mock exe: %v", err)
	}

	os.Setenv("ENOUGH_BROWSER_EXECUTABLE", exePath)
	defer os.Unsetenv("ENOUGH_BROWSER_EXECUTABLE")

	resolved, err := resolveBrowserExecutable()
	if err != nil {
		t.Fatalf("failed to resolve: %v", err)
	}
	if resolved != exePath {
		t.Errorf("expected %s, got %s", exePath, resolved)
	}
}
