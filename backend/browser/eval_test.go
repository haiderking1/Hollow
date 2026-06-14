package browser

import (
	"strings"
	"testing"
)

func TestValidateCssSelector(t *testing.T) {
	if err := ValidateCssSelector("button.primary"); err != nil {
		t.Errorf("expected button.primary to be valid, got error: %v", err)
	}

	err := ValidateCssSelector("a:contains('x')")
	if err == nil {
		t.Errorf("expected a:contains('x') to be invalid")
	} else if !strings.Contains(err.Error(), "jQuery syntax") {
		t.Errorf("expected jQuery syntax error message, got: %v", err.Error())
	}
}

func TestLooksLikeCssSelectorOnly(t *testing.T) {
	if !LooksLikeCssSelectorOnly(".btn-hero-primary") {
		t.Errorf("expected .btn-hero-primary to look like CSS selector only")
	}
	if LooksLikeCssSelectorOnly("document.querySelector('.x')") {
		t.Errorf("expected document.querySelector('.x') not to look like CSS selector only")
	}
}

func TestResolveClickPlan(t *testing.T) {
	// 1. Selector set
	sel := ".btn-hero-primary"
	plan, err := ResolveClickPlan(nil, &sel, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil || plan.Selector == nil || *plan.Selector != sel || plan.Index != 0 {
		t.Errorf("expected plan for selector %s index 0, got %v", sel, plan)
	}

	// 2. Bare CSS expression
	expr1 := ".btn-hero-primary"
	plan, err = ResolveClickPlan(&expr1, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil || plan.Selector == nil || *plan.Selector != expr1 || plan.Index != 0 {
		t.Errorf("expected plan for bare css %s index 0, got %v", expr1, plan)
	}

	// 3. Index only click
	idx := 22
	plan, err = ResolveClickPlan(nil, nil, &idx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil || plan.Selector != nil || plan.Index != idx {
		t.Errorf("expected plan for index only %d, got %v", idx, plan)
	}

	// 4. querySelector rewrite
	expr2 := "document.querySelector('.btn-hero-primary').click()"
	plan, err = ResolveClickPlan(&expr2, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil || plan.Selector == nil || *plan.Selector != ".btn-hero-primary" || plan.Index != 0 {
		t.Errorf("expected plan for querySelector rewrite, got %v", plan)
	}
}

func TestFormatEvalResultText(t *testing.T) {
	text := FormatEvalResultText(ClickFeedback{
		Clicked:    true,
		Tag:        "A",
		ClassName:  "download",
		Text:       "Download for Windows",
		Selector:   "a.download",
		Index:      intPtr(0),
		MatchCount: intPtr(1),
		Download: &DownloadInfo{
			Started:  true,
			Filename: "fdm_x64_setup.exe",
			URL:      "https://example.com/setup.exe",
		},
	})

	if !strings.Contains(text, "Clicked element:") {
		t.Errorf("expected 'Clicked element:' in formatted text")
	}
	if !strings.Contains(text, "download started: fdm_x64_setup.exe") {
		t.Errorf("expected 'download started: fdm_x64_setup.exe' in formatted text")
	}

	textNilIndex := FormatEvalResultText(ClickFeedback{Clicked: true, Tag: "A", Text: "Go"})
	if strings.Contains(textNilIndex, "index:") {
		t.Fatal("should not print index line when Index is nil")
	}
}

func intPtr(val int) *int {
	return &val
}

func strPtr(val string) *string {
	return &val
}

func TestResolveEvalExpressionBareClickRejection(t *testing.T) {
	_, err := ResolveEvalExpression(strPtr("foo.click()"), nil, nil)
	if err == nil {
		t.Fatalf("expected error for bare click expression")
	}
	if !strings.Contains(err.Error(), "Bare .click() expressions return undefined") {
		t.Errorf("expected bare click error message, got: %v", err)
	}

	plan, err := ResolveClickPlan(strPtr("document.querySelectorAll('.btn')[2].click()"), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan == nil || plan.Selector == nil || *plan.Selector != ".btn" || plan.Index != 2 {
		t.Errorf("expected plan for querySelectorAll rewrite, got: %v", plan)
	}
}
