package browser

import (
	"fmt"
	"strings"
	"time"
)

const (
	CloudflareWaitTimeout  = 5 * time.Second
	CloudflarePollInterval = 250 * time.Millisecond
)

const cloudflareChallengeExpression = `(() => {
	const title = document.title || "";
	const text = document.body ? (document.body.innerText || document.body.textContent || "") : "";
	const html = document.documentElement ? (document.documentElement.outerHTML || "") : "";
	const combined = (title + "\n" + text + "\n" + html).toLowerCase();
	const markers = [
		"just a moment",
		"checking your browser",
		"security by cloudflare",
		"malicious bots",
		"cf-browser-verification",
		"challenge-platform",
		"/cdn-cgi/challenge-platform/",
		"cf-chl-widget",
		"challenge-running",
		"enable javascript and cookies to continue",
		"verify you are human",
		"needs to review the security of your connection",
	];
	if (markers.some((m) => combined.includes(m))) {
		return true;
	}
	return !!document.querySelector(
		"#challenge-running, #cf-wrapper, #cf-challenge-running, .cf-turnstile, [data-translate='checking_browser']"
	);
})()`

// IsCloudflareChallengeText reports common Cloudflare interstitial copy in scraped text.
func IsCloudflareChallengeText(text string) bool {
	combined := strings.ToLower(text)
	markers := []string{
		"just a moment",
		"checking your browser",
		"security by cloudflare",
		"malicious bots",
		"cf-browser-verification",
		"challenge-platform",
		"enable javascript and cookies to continue",
		"verify you are human",
		"needs to review the security of your connection",
	}
	for _, marker := range markers {
		if strings.Contains(combined, marker) {
			return true
		}
	}
	return false
}

func isCloudflareChallenge(session *CdpSession) (bool, error) {
	val, err := EvaluateExpression(session, cloudflareChallengeExpression, false)
	if err != nil {
		return false, err
	}
	challenged, ok := val.(bool)
	if !ok {
		return false, nil
	}
	return challenged, nil
}

// waitForCloudflareClearance waits for a Cloudflare interstitial to finish.
// When a challenge is detected, it sleeps CloudflareWaitTimeout to let the
// bot check run, then polls until the challenge clears or times out again.
func waitForCloudflareClearance(session *CdpSession) error {
	challenged, err := isCloudflareChallenge(session)
	if err != nil {
		return err
	}
	if !challenged {
		return nil
	}

	time.Sleep(CloudflareWaitTimeout)

	deadline := time.Now().Add(CloudflareWaitTimeout)
	for time.Now().Before(deadline) {
		challenged, err = isCloudflareChallenge(session)
		if err != nil {
			return err
		}
		if !challenged {
			return nil
		}
		time.Sleep(CloudflarePollInterval)
	}
	return fmt.Errorf("cloudflare challenge did not complete within %s", CloudflareWaitTimeout*2)
}

func waitForPageReady(session *CdpSession, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		val, err := EvaluateExpression(session, "document.readyState", false)
		if err == nil {
			if str, ok := val.(string); ok && (str == "complete" || str == "interactive") {
				return
			}
		}
		time.Sleep(CloudflarePollInterval)
	}
}
