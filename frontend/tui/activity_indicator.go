package tui

import (
	"fmt"
	"time"
)

type activityPhase int

const (
	activityIdle activityPhase = iota
	activityConnecting
	activityStreaming
)

var activityBreatheFrames = []string{"○", "◔", "◑", "◕", "◉", "◕", "◑", "◔"}

const (
	activityFrameHoldTicks  = 3
	activityWordAdvanceTick = 24
)

var streamingActivityWords = []string{
	"Cocking",
	"Simmering",
	"Reducing",
	"Seasoning",
	"Plating",
	"Whisking",
	"Basting",
	"Marinating",
	"Caramelizing",
	"Fermenting",
	"Distilling",
	"Glazing",
	"Proofing",
	"Kneading",
	"Tempering",
	"Quantumizing",
	"Synthesizing",
	"Composing",
	"Assembling",
	"Processing",
}

func (a *App) beginAgentActivity() {
	a.activityPhase = activityConnecting
	a.activityFrame = 0
	a.activityStreamSegments = 0
	a.activityStartedAt = time.Now()
	a.activityWordIndex = a.nextActivityWord()
}

func (a *App) stopAgentActivity() {
	a.activityPhase = activityIdle
	a.activityFrame = 0
	a.activityStreamSegments = 0
	a.activityStartedAt = time.Time{}
}

func (a *App) onAssistantStreamStart() {
	if a.activityPhase == activityIdle {
		a.beginAgentActivity()
	}
	if a.activityStreamSegments > 0 {
		a.advanceActivityWord()
	}
	a.activityPhase = activityStreaming
	a.activityFrame = 0
	a.activityStreamSegments++
	a.lastActivityWordIndex = normalizedActivityWordIndex(a.activityWordIndex)
}

func (a *App) agentActivityVisible() bool {
	return a.activityPhase == activityConnecting || a.activityPhase == activityStreaming
}

func (a *App) tickAgentActivity() {
	if !a.agentActivityVisible() {
		return
	}
	a.activityFrame++
	if a.activityPhase == activityStreaming && a.activityFrame > 0 && a.activityFrame%activityWordAdvanceTick == 0 {
		a.advanceActivityWord()
	}
}

func (a *App) advanceActivityWord() {
	if len(streamingActivityWords) == 0 {
		return
	}
	a.activityWordIndex = normalizedActivityWordIndex(a.activityWordIndex + 1)
	a.lastActivityWordIndex = a.activityWordIndex
}

func (a *App) nextActivityWord() int {
	if len(streamingActivityWords) == 0 {
		return 0
	}
	idx := normalizedActivityWordIndex(a.nextActivityWordIndex)
	if idx == normalizedActivityWordIndex(a.lastActivityWordIndex) {
		idx = normalizedActivityWordIndex(idx + 1)
	}
	a.nextActivityWordIndex = normalizedActivityWordIndex(idx + 1)
	return idx
}

func normalizedActivityWordIndex(idx int) int {
	if len(streamingActivityWords) == 0 {
		return 0
	}
	idx %= len(streamingActivityWords)
	if idx < 0 {
		idx += len(streamingActivityWords)
	}
	return idx
}

func activityWordAt(idx int) string {
	if len(streamingActivityWords) == 0 {
		return ""
	}
	return streamingActivityWords[normalizedActivityWordIndex(idx)]
}

func activityBreatheFrameAt(tick int) string {
	if tick < 0 {
		tick = 0
	}
	return activityBreatheFrames[(tick/activityFrameHoldTicks)%len(activityBreatheFrames)]
}

func formatActivityElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	minutes := total / 60
	seconds := total % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	hours := minutes / 60
	minutes %= 60
	return fmt.Sprintf("%dh%02dm", hours, minutes)
}

func (a *App) activityLabel() string {
	switch a.activityPhase {
	case activityConnecting:
		return "Working"
	case activityStreaming:
		return activityWordAt(a.activityWordIndex)
	default:
		return ""
	}
}

func (a *App) renderAgentActivityLoader() string {
	label := a.activityLabel()
	if label == "" {
		return ""
	}
	bullet := a.styles.ActivityBullet.Render(activityBreatheFrameAt(a.activityFrame))
	text := a.styles.ActivityText.Render(label)
	elapsed := "0s"
	if !a.activityStartedAt.IsZero() {
		elapsed = formatActivityElapsed(time.Since(a.activityStartedAt))
	}
	timer := a.styles.LogDim.Render("(" + elapsed + ")")
	return bullet + " " + text + " " + timer
}
