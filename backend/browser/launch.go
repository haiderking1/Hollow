package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/session"
)

const (
	LaunchTimeoutMS = 20000
	LaunchPollMS    = 250
	DefaultCDPPort  = 9222
)

var (
	launchMu    sync.Mutex
	launchChans = make(map[string]chan error)
)

func resetBrowserLaunchStateForTests() {
	launchMu.Lock()
	defer launchMu.Unlock()
	launchChans = make(map[string]chan error)
}

func ShouldAutoLaunchBrowser() bool {
	return os.Getenv("ENOUGH_BROWSER_AUTO_LAUNCH") != "0"
}

func getBrowserProfileDir() (string, error) {
	override := strings.TrimSpace(os.Getenv("ENOUGH_BROWSER_PROFILE_DIR"))
	if override != "" {
		if err := os.MkdirAll(override, 0700); err != nil {
			return "", err
		}
		return override, nil
	}
	home, err := session.HomeAgentDir()
	if err != nil {
		return "", err
	}
	profileDir := filepath.Join(home, "browser-profile")
	if err := os.MkdirAll(profileDir, 0700); err != nil {
		return "", err
	}
	return profileDir, nil
}

func parseCdpPort(baseUrl string) int {
	parsed, err := url.Parse(baseUrl)
	if err != nil {
		return DefaultCDPPort
	}
	portStr := parsed.Port()
	if portStr == "" {
		return DefaultCDPPort
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return DefaultCDPPort
	}
	return port
}

func commandExists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

func resolveBrowserExecutable() (string, error) {
	override := strings.TrimSpace(os.Getenv("ENOUGH_BROWSER_EXECUTABLE"))
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("ENOUGH_BROWSER_EXECUTABLE not found: %s", override)
		}
		return override, nil
	}

	switch runtime.GOOS {
	case "windows":
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = "C:\\Program Files"
		}
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		if programFilesX86 == "" {
			programFilesX86 = "C:\\Program Files (x86)"
		}
		candidates := []string{
			filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	case "darwin":
		candidates := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
	case "linux":
		commands := []string{
			"google-chrome-stable",
			"google-chrome",
			"chromium-browser",
			"chromium",
			"microsoft-edge",
		}
		for _, cmd := range commands {
			if commandExists(cmd) {
				return cmd, nil
			}
		}
		return commands[0], nil
	}

	return "", fmt.Errorf("No Chrome or Edge executable found. Install Chrome/Edge or set ENOUGH_BROWSER_EXECUTABLE to the browser binary path.")
}

func isCdpConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fetch failed") ||
		strings.Contains(msg, "econnrefused") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "unable to connect") ||
		strings.Contains(msg, "network") ||
		strings.Contains(msg, "timed out")
}

func formatCdpConnectionError(baseUrl string, err error, launched bool) string {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	profileDir, _ := getBrowserProfileDir()
	manual := fmt.Sprintf("Start Chrome/Edge manually, e.g. chrome --remote-debugging-port=%d --user-data-dir=\"%s\"", parseCdpPort(baseUrl), profileDir)
	if launched {
		return fmt.Sprintf("Could not connect to browser CDP at %s after auto-launch (%s). %s", baseUrl, detail, manual)
	}
	if ShouldAutoLaunchBrowser() {
		return fmt.Sprintf("Could not connect to browser CDP at %s (%s). Auto-launch was attempted but failed. %s", baseUrl, detail, manual)
	}
	return fmt.Sprintf("Could not connect to browser CDP at %s (%s). Set ENOUGH_BROWSER_AUTO_LAUNCH=1 (default) or %s", baseUrl, detail, manual)
}

func waitForCdpReady(baseUrl string, timeoutMs int) (bool, error) {
	parsed, err := url.Parse(baseUrl)
	if err != nil {
		return false, err
	}
	parsed.Path = "/json/version"
	versionUrl := parsed.String()

	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), "GET", versionUrl, nil)
		if err == nil {
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true, nil
				}
			}
		}
		time.Sleep(LaunchPollMS * time.Millisecond)
	}
	return false, nil
}

func launchBrowserOnce(baseUrl string) error {
	executable, err := resolveBrowserExecutable()
	if err != nil {
		return err
	}
	port := parseCdpPort(baseUrl)
	profileDir, err := getBrowserProfileDir()
	if err != nil {
		return err
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		fmt.Sprintf("--user-data-dir=%s", profileDir),
		"--no-first-run",
		"--no-default-browser-check",
		"about:blank",
	}

	cmd := exec.Command(executable, args...)
	detachProcess(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}

	ready, err := waitForCdpReady(baseUrl, LaunchTimeoutMS)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("Launched %s but CDP did not become ready on %s within %dms", executable, baseUrl, LaunchTimeoutMS)
	}
	return nil
}

func ensureBrowserLaunched(baseUrl string) (bool, error) {
	if !ShouldAutoLaunchBrowser() {
		return false, nil
	}

	launchMu.Lock()
	ch, exists := launchChans[baseUrl]
	if !exists {
		ch = make(chan error, 1)
		launchChans[baseUrl] = ch
		launchMu.Unlock()

		err := launchBrowserOnce(baseUrl)
		ch <- err
		close(ch)
		return true, err
	}
	launchMu.Unlock()

	err := <-ch
	return true, err
}
