package browser

import (
	"encoding/json"
	"fmt"
)

const MaxElementList = 50

func buildScrapeExpression(selector *string, format string) (string, error) {
	clickableSelector := `a[href], button, [role="button"], input[type="submit"], input[type="button"], [onclick]`
	clickableSelectorJson, _ := json.Marshal(clickableSelector)

	if format == "elements" {
		if selector != nil {
			selectorJson, err := json.Marshal(*selector)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(`(() => {
				let matches;
				try {
					matches = document.querySelectorAll(%s);
				} catch (error) {
					const message = error instanceof Error ? error.message : String(error);
					throw new Error("Invalid CSS selector: " + %s + ". " + message + " (:contains() is jQuery, not CSS.)");
				}
				return Array.from(matches).slice(0, %d).map((el, index) => ({
					index,
					tag: el.tagName,
					id: el.id || undefined,
					className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
					href: el.href || el.getAttribute("href") || undefined,
					text: (el.textContent || "").trim().slice(0, 200) || undefined,
				}));
			})()`, string(selectorJson), string(selectorJson), MaxElementList), nil
		}
		return fmt.Sprintf(`(() => {
			const candidates = Array.from(document.querySelectorAll(%s));
			return candidates.slice(0, %d).map((el, index) => ({
				index,
				tag: el.tagName,
				id: el.id || undefined,
				className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
				href: el.href || el.getAttribute("href") || undefined,
				text: (el.textContent || "").trim().slice(0, 200) || undefined,
				role: el.getAttribute("role") || undefined,
				type: el.getAttribute("type") || undefined,
			}));
		})()`, string(clickableSelectorJson), MaxElementList), nil
	}

	if format == "html" {
		target := "document.documentElement"
		if selector != nil {
			selectorJson, err := json.Marshal(*selector)
			if err != nil {
				return "", err
			}
			target = fmt.Sprintf("document.querySelector(%s)", string(selectorJson))
		}
		return fmt.Sprintf(`(() => { const node = %s; if (!node) return null; return node.outerHTML ?? node.textContent ?? ""; })()`, target), nil
	}

	if format == "links" {
		root := "document"
		if selector != nil {
			selectorJson, err := json.Marshal(*selector)
			if err != nil {
				return "", err
			}
			root = fmt.Sprintf("document.querySelector(%s)", string(selectorJson))
		}
		return fmt.Sprintf(`(() => {
			const root = %s;
			if (!root) return [];
			const anchors = root.querySelectorAll ? root.querySelectorAll("a[href]") : [];
			return Array.from(anchors).map((a) => ({
				href: a.href,
				text: (a.textContent || "").trim(),
			}));
		})()`, root), nil
	}

	// Default to "text"
	target := "document.body"
	if selector != nil {
		selectorJson, err := json.Marshal(*selector)
		if err != nil {
			return "", err
		}
		target = fmt.Sprintf("document.querySelector(%s)", string(selectorJson))
	}
	return fmt.Sprintf(`(() => { const node = %s; if (!node) return null; return node.innerText ?? node.textContent ?? ""; })()`, target), nil
}
