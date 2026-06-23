// PORT: backend/browser/cloudflare.go

import { Effect } from "effect";
import { type CdpSession, EvaluateExpression } from "./cdp";

export const CloudflareWaitTimeout = 5000; // 5 seconds in milliseconds
export const CloudflarePollInterval = 250; // 250 milliseconds

export const cloudflareChallengeExpression = `(() => {
	const title = document.title || "";
	const text = document.body ? (document.body.innerText || document.body.textContent || "") : "";
	const html = document.documentElement ? (document.documentElement.outerHTML || "") : "";
	const combined = (title + "\\n" + text + "\\n" + html).toLowerCase();
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
})()`;

// IsCloudflareChallengeText reports common Cloudflare interstitial copy in scraped text.
export function IsCloudflareChallengeText(text: string): boolean {
  const combined = text.toLowerCase();
  const markers = [
    "just a moment",
    "checking your browser",
    "security by cloudflare",
    "malicious bots",
    "cf-browser-verification",
    "challenge-platform",
    "enable javascript and cookies to continue",
    "verify you are human",
    "needs to review the security of your connection",
  ];
  return markers.some((m) => combined.includes(m));
}

export function isCloudflareChallenge(session: CdpSession): Effect.Effect<boolean, Error> {
  return EvaluateExpression(session, cloudflareChallengeExpression, false).pipe(
    Effect.map((val) => {
      if (typeof val === "boolean") {
        return val;
      }
      return false;
    })
  );
}

// waitForCloudflareClearance waits for a Cloudflare interstitial to finish.
// When a challenge is detected, it sleeps CloudflareWaitTimeout to let the
// bot check run, then polls until the challenge clears or times out again.
export function waitForCloudflareClearance(session: CdpSession): Effect.Effect<void, Error> {
  return isCloudflareChallenge(session).pipe(
    Effect.flatMap((challenged) => {
      if (!challenged) {
        return Effect.void;
      }

      return Effect.sleep(CloudflareWaitTimeout).pipe(
        Effect.flatMap(() => {
          const deadline = Date.now() + CloudflareWaitTimeout;

          const poll = (): Effect.Effect<void, Error> => {
            return isCloudflareChallenge(session).pipe(
              Effect.flatMap((stillChallenged) => {
                if (!stillChallenged) {
                  return Effect.void;
                }
                if (Date.now() >= deadline) {
                  return Effect.fail(
                    new Error(`cloudflare challenge did not complete within ${CloudflareWaitTimeout * 2}`)
                  );
                }
                return Effect.sleep(CloudflarePollInterval).pipe(Effect.flatMap(() => poll()));
              })
            );
          };

          return poll();
        })
      );
    })
  );
}

export function waitForPageReady(session: CdpSession, timeoutMs: number): Effect.Effect<void, never> {
  const deadline = Date.now() + timeoutMs;

  const poll = (): Effect.Effect<void, never> => {
    return EvaluateExpression(session, "document.readyState", false).pipe(
      Effect.matchEffect({
        onFailure: () => {
          if (Date.now() >= deadline) {
            return Effect.void;
          }
          return Effect.sleep(CloudflarePollInterval).pipe(Effect.flatMap(() => poll()));
        },
        onSuccess: (val) => {
          if (typeof val === "string" && (val === "complete" || val === "interactive")) {
            return Effect.void;
          }
          if (Date.now() >= deadline) {
            return Effect.void;
          }
          return Effect.sleep(CloudflarePollInterval).pipe(Effect.flatMap(() => poll()));
        },
      })
    );
  };

  return poll();
}

/*
PORT STATUS
source path: backend/browser/cloudflare.go
source lines: 115
draft lines: 122
confidence: high
status: phase_b_compile
*/
