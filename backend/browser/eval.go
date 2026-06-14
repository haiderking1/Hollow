package browser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	containsRe    = regexp.MustCompile(`(?i):contains\s*\(`)
	allSingleRe   = regexp.MustCompile(`^document\.querySelectorAll\(\s*'([\s\S]*?)'\s*\)\s*\[\s*(\d+)\s*\]\.click\(\)\s*$`)
	oneSingleRe   = regexp.MustCompile(`^document\.querySelector\(\s*'([\s\S]*?)'\s*\)\.click\(\)\s*$`)
	allDoubleRe   = regexp.MustCompile(`^document\.querySelectorAll\(\s*"([\s\S]*?)"\s*\)\s*\[\s*(\d+)\s*\]\.click\(\)\s*$`)
	oneDoubleRe   = regexp.MustCompile(`^document\.querySelector\(\s*"([\s\S]*?)"\s*\)\.click\(\)\s*$`)
	allBacktickRe = regexp.MustCompile("^document\\.querySelectorAll\\(\\s*`([\\s\\S]*?)`\\s*\\)\\s*\\[\\s*(\\d+)\\s*\\]\\.click\\(\\)\\s*$")
	oneBacktickRe = regexp.MustCompile("^document\\.querySelector\\(\\s*`([\\s\\S]*?)`\\s*\\)\\.click\\(\\)\\s*$")
	kwRe          = regexp.MustCompile(`(?i)^(document|window|return|function|const|let|var|if|for|while|async|await)\b`)
	startRe       = regexp.MustCompile(`^([.#\[]|[a-zA-Z_*])`)
	clickEndRe    = regexp.MustCompile(`\b\.click\s*\(\s*\)\s*$`)
)

func LooksLikeCssSelectorOnly(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "(") || strings.Contains(trimmed, ";") {
		return false
	}
	if kwRe.MatchString(trimmed) {
		return false
	}
	return startRe.MatchString(trimmed)
}

func ParseQuerySelectorClickExpression(expression string) (string, int, bool) {
	trimmed := strings.TrimSpace(expression)
	if m := allSingleRe.FindStringSubmatch(trimmed); m != nil {
		idx, _ := strconv.Atoi(m[2])
		return m[1], idx, true
	}
	if m := oneSingleRe.FindStringSubmatch(trimmed); m != nil {
		return m[1], 0, true
	}
	if m := allDoubleRe.FindStringSubmatch(trimmed); m != nil {
		idx, _ := strconv.Atoi(m[2])
		return m[1], idx, true
	}
	if m := oneDoubleRe.FindStringSubmatch(trimmed); m != nil {
		return m[1], 0, true
	}
	if m := allBacktickRe.FindStringSubmatch(trimmed); m != nil {
		idx, _ := strconv.Atoi(m[2])
		return m[1], idx, true
	}
	if m := oneBacktickRe.FindStringSubmatch(trimmed); m != nil {
		return m[1], 0, true
	}
	return "", 0, false
}

func ValidateCssSelector(selector string) error {
	if containsRe.MatchString(selector) {
		return fmt.Errorf(`Invalid CSS selector "%s": :contains() is jQuery syntax, not valid CSS. Use scrape format=elements to list matches, then click with a standard selector and optional index.`, selector)
	}
	return nil
}

func ResolveClickPlan(expression *string, selector *string, index *int) (*ClickPlan, error) {
	if selector != nil && *selector != "" {
		if err := ValidateCssSelector(*selector); err != nil {
			return nil, err
		}
		idx := 0
		if index != nil {
			idx = *index
		}
		return &ClickPlan{Selector: selector, Index: idx}, nil
	}

	var expr string
	if expression != nil {
		expr = strings.TrimSpace(*expression)
	}

	if expr == "" {
		if index != nil {
			return &ClickPlan{Index: *index}, nil
		}
		return nil, nil
	}

	if LooksLikeCssSelectorOnly(expr) {
		if err := ValidateCssSelector(expr); err != nil {
			return nil, err
		}
		idx := 0
		if index != nil {
			idx = *index
		}
		return &ClickPlan{Selector: &expr, Index: idx}, nil
	}

	if sel, idx, ok := ParseQuerySelectorClickExpression(expr); ok {
		if err := ValidateCssSelector(sel); err != nil {
			return nil, err
		}
		return &ClickPlan{Selector: &sel, Index: idx}, nil
	}

	return nil, nil
}

func ResolveEvalExpression(expression *string, selector *string, index *int) (string, error) {
	plan, err := ResolveClickPlan(expression, selector, index)
	if err != nil {
		return "", err
	}
	if plan != nil {
		return "", fmt.Errorf("Internal error: click plans must use clickElementWithFeedback")
	}

	var expr string
	if expression != nil {
		expr = strings.TrimSpace(*expression)
	}
	if expr == "" {
		return "", fmt.Errorf("expression, selector, or index is required for eval action")
	}

	if clickEndRe.MatchString(expr) {
		return "", fmt.Errorf("Bare .click() expressions return undefined. Use selector (e.g. selector=.btn-hero-primary) or index from scrape format=elements instead of a raw click expression.")
	}

	return expr, nil
}

func FormatEvalResultText(result interface{}) string {
	if result == nil {
		return "undefined"
	}

	var feedback ClickFeedback
	isFeedback := false

	if f, ok := result.(ClickFeedback); ok {
		feedback = f
		isFeedback = true
	} else if f, ok := result.(*ClickFeedback); ok && f != nil {
		feedback = *f
		isFeedback = true
	}

	if isFeedback && feedback.Clicked {
		var lines []string
		lines = append(lines, "Clicked element:")
		if feedback.Tag != "" {
			lines = append(lines, fmt.Sprintf("  tag: %s", feedback.Tag))
		}
		if feedback.ClassName != "" {
			lines = append(lines, fmt.Sprintf("  class: %s", feedback.ClassName))
		}
		if feedback.Text != "" {
			lines = append(lines, fmt.Sprintf("  text: %s", feedback.Text))
		}
		if feedback.Href != "" {
			lines = append(lines, fmt.Sprintf("  href: %s", feedback.Href))
		}
		if feedback.Selector != "" {
			lines = append(lines, fmt.Sprintf("  selector: %s", feedback.Selector))
		}
		if feedback.Index != nil {
			idxLine := fmt.Sprintf("  index: %d", *feedback.Index)
			if feedback.MatchCount != nil {
				idxLine += fmt.Sprintf(" of %d", *feedback.MatchCount)
			}
			lines = append(lines, idxLine)
		}
		if feedback.Download != nil && feedback.Download.Started {
			filename := feedback.Download.Filename
			if filename == "" {
				filename = "unknown file"
			}
			dlLine := fmt.Sprintf("  download started: %s", filename)
			if feedback.Download.URL != "" {
				dlLine += fmt.Sprintf("\n  download url: %s", feedback.Download.URL)
			}
			lines = append(lines, dlLine)
		}
		return strings.Join(lines, "\n")
	}

	if str, ok := result.(string); ok {
		return str
	}

	bytes, err := json.MarshalIndent(result, "", "  ")
	if err == nil {
		return string(bytes)
	}

	return fmt.Sprintf("%v", result)
}
