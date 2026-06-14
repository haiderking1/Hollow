package browser

import (
	"encoding/json"
	"fmt"
	"time"
)

type ClickPlan struct {
	Selector *string
	Index    int
}

type ClickProbe struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Tag        string  `json:"tag"`
	ID         string  `json:"id,omitempty"`
	ClassName  string  `json:"className,omitempty"`
	Href       string  `json:"href,omitempty"`
	Text       string  `json:"text,omitempty"`
	Selector   string  `json:"selector,omitempty"`
	Index      *int    `json:"index,omitempty"`
	MatchCount *int    `json:"matchCount,omitempty"`
}

type ClickFeedback struct {
	Clicked    bool          `json:"clicked"`
	X          float64       `json:"x"`
	Y          float64       `json:"y"`
	Tag        string        `json:"tag"`
	ID         string        `json:"id,omitempty"`
	ClassName  string        `json:"className,omitempty"`
	Href       string        `json:"href,omitempty"`
	Text       string        `json:"text,omitempty"`
	Selector   string        `json:"selector,omitempty"`
	Index      *int          `json:"index,omitempty"`
	MatchCount *int          `json:"matchCount,omitempty"`
	Download   *DownloadInfo `json:"download,omitempty"`
}

type DownloadInfo struct {
	Started  bool   `json:"started"`
	Filename string `json:"filename,omitempty"`
	URL      string `json:"url,omitempty"`
}

func buildClickProbeExpression(plan ClickPlan) (string, error) {
	clickableSelector := `a[href], button, [role="button"], input[type="submit"], input[type="button"], [onclick]`
	clickableSelectorJson, _ := json.Marshal(clickableSelector)

	if plan.Selector != nil {
		selectorJson, err := json.Marshal(*plan.Selector)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`(() => {
			let matches;
			try {
				matches = document.querySelectorAll(%s);
			} catch (error) {
				const message = error instanceof Error ? error.message : String(error);
				throw new Error("Invalid CSS selector: " + %s + ". " + message);
			}
			const matchCount = matches.length;
			if (matchCount === 0) {
				throw new Error("Selector not found: " + %s);
			}
			const index = %d;
			if (index < 0 || index >= matchCount) {
				throw new Error("Selector " + %s + " matched " + matchCount + " element(s), but index " + index + " is out of range");
			}
			const el = matches[index];
			el.scrollIntoView({ block: "center", inline: "center" });
			const rect = el.getBoundingClientRect();
			return JSON.stringify({
				x: rect.left + rect.width / 2,
				y: rect.top + rect.height / 2,
				tag: el.tagName,
				id: el.id || undefined,
				className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
				href: el.href || el.getAttribute("href") || undefined,
				text: (el.textContent || "").trim().slice(0, 200) || undefined,
				selector: %s,
				index,
				matchCount,
			});
		})()`, string(selectorJson), string(selectorJson), string(selectorJson), plan.Index, string(selectorJson), string(selectorJson)), nil
	}

	return fmt.Sprintf(`(() => {
		const candidates = Array.from(document.querySelectorAll(%s));
		const index = %d;
		if (index < 0 || index >= candidates.length) {
			throw new Error("Element index " + index + " is out of range (" + candidates.length + " clickable elements)");
		}
		const el = candidates[index];
		el.scrollIntoView({ block: "center", inline: "center" });
		const rect = el.getBoundingClientRect();
		return JSON.stringify({
			x: rect.left + rect.width / 2,
			y: rect.top + rect.height / 2,
			tag: el.tagName,
			id: el.id || undefined,
			className: typeof el.className === "string" && el.className.length > 0 ? el.className : undefined,
			href: el.href || el.getAttribute("href") || undefined,
			text: (el.textContent || "").trim().slice(0, 200) || undefined,
			index,
			matchCount: candidates.length,
		});
	})()`, string(clickableSelectorJson), plan.Index), nil
}

func parseClickProbe(raw interface{}) (ClickProbe, error) {
	var probe ClickProbe
	str, ok := raw.(string)
	if ok {
		if err := json.Unmarshal([]byte(str), &probe); err != nil {
			return ClickProbe{}, err
		}
		return probe, nil
	}

	bytes, err := json.Marshal(raw)
	if err != nil {
		return ClickProbe{}, err
	}
	if err := json.Unmarshal(bytes, &probe); err != nil {
		return ClickProbe{}, err
	}
	return probe, nil
}

func dispatchMouseClick(session *CdpSession, x, y float64) error {
	type mouseParams struct {
		X          float64 `json:"x"`
		Y          float64 `json:"y"`
		Button     string  `json:"button"`
		ClickCount int     `json:"clickCount"`
		Type       string  `json:"type"`
	}

	params := mouseParams{
		X:          x,
		Y:          y,
		Button:     "left",
		ClickCount: 1,
	}

	params.Type = "mousePressed"
	_, err := session.Send("Input.dispatchMouseEvent", params)
	if err != nil {
		return err
	}

	params.Type = "mouseReleased"
	_, err = session.Send("Input.dispatchMouseEvent", params)
	return err
}

func waitForDownloadBegin(session *CdpSession, timeout time.Duration) (*DownloadInfo, error) {
	ch := make(chan *DownloadInfo, 1)

	unsubscribe := session.OnEvent("Page.downloadWillBegin", func(params interface{}) {
		var event struct {
			SuggestedFilename string `json:"suggestedFilename"`
			URL               string `json:"url"`
		}
		bytes, err := json.Marshal(params)
		if err == nil {
			_ = json.Unmarshal(bytes, &event)
		}

		ch <- &DownloadInfo{
			Started:  true,
			Filename: event.SuggestedFilename,
			URL:      event.URL,
		}
	})
	defer unsubscribe()

	select {
	case dl := <-ch:
		return dl, nil
	case <-time.After(timeout):
		return nil, nil
	}
}

func clickElementWithFeedback(session *CdpSession, plan ClickPlan) (ClickFeedback, error) {
	_, err := session.Send("Page.enable", nil)
	if err != nil {
		return ClickFeedback{}, err
	}

	expr, err := buildClickProbeExpression(plan)
	if err != nil {
		return ClickFeedback{}, err
	}

	evalVal, err := EvaluateExpression(session, expr, false)
	if err != nil {
		return ClickFeedback{}, err
	}

	probe, err := parseClickProbe(evalVal)
	if err != nil {
		return ClickFeedback{}, err
	}

	dlChan := make(chan *DownloadInfo, 1)
	go func() {
		dl, _ := waitForDownloadBegin(session, 3000*time.Millisecond)
		dlChan <- dl
	}()

	err = dispatchMouseClick(session, probe.X, probe.Y)
	if err != nil {
		return ClickFeedback{}, err
	}

	dl := <-dlChan

	feedback := ClickFeedback{
		Clicked:    true,
		X:          probe.X,
		Y:          probe.Y,
		Tag:        probe.Tag,
		ID:         probe.ID,
		ClassName:  probe.ClassName,
		Href:       probe.Href,
		Text:       probe.Text,
		Selector:   probe.Selector,
		Index:      probe.Index,
		MatchCount: probe.MatchCount,
		Download:   dl,
	}
	return feedback, nil
}
