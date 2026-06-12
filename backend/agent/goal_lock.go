package agent

import (
	"fmt"
	"strings"

	"github.com/enough/enough/backend/core"
)

func goalLockNotice(lockedGoal string) string {
	var b strings.Builder
	b.WriteString(core.RuntimeNoticePrefix)
	b.WriteString("GOAL LOCK — complete exactly this user task this turn.\n")
	b.WriteString("Do not pivot scope, propose alternatives, or declare done without verification.\n")
	b.WriteString("If blocked, try a different execution path on the SAME goal.\n\n")
	b.WriteString(lockedGoal)
	return b.String()
}

func parallelForkNotice(forkCount int, lockedGoal, summary string) string {
	var b strings.Builder
	b.WriteString(core.RuntimeNoticePrefix)
	fmt.Fprintf(&b, "PARALLEL FORKS — %d same-model attempts ran on the locked goal after repeated verify failures.\n", forkCount)
	b.WriteString(summary)
	b.WriteString("\n\n")
	b.WriteString(lockedGoal)
	return b.String()
}

func goalLockReminder(lockedGoal string) string {
	if strings.TrimSpace(lockedGoal) == "" {
		return ""
	}
	return "\nGOAL LOCK: " + lockedGoal
}
