package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/enough/enough/frontend/tui/markdown"
	"github.com/enough/enough/frontend/tui/term"
)

type toolKind string

const (
	toolKindWrite toolKind = "write"
	toolKindEdit  toolKind = "edit"
	toolKindRead  toolKind = "read"
	toolKindBash  toolKind = "bash"
	toolKindSwarm toolKind = "swarm"
	toolKindWeb   toolKind = "web"
	toolKindOther toolKind = "other"
)

type toolRow struct {
	Kind     toolKind
	Name     string
	Args     string
	Action   string
	Target   string
	Added    int
	Removed  int
	Lines    int
	Pending  bool
	Error    bool
	Output   string
	Metadata string
}

func parseToolRow(msg chatMsg) toolRow {
	name := msg.toolName
	if name == "" {
		name = "tool"
	}

	var args map[string]json.RawMessage
	_ = json.Unmarshal([]byte(msg.toolArgs), &args)

	row := toolRow{
		Name:     name,
		Args:     msg.toolArgs,
		Pending:  msg.toolPending,
		Error:    msg.toolError,
		Output:   strings.TrimSpace(msg.toolResult),
		Metadata: msg.toolDetails,
		Added:    msg.toolAdded,
		Removed:  msg.toolRemoved,
	}

	switch name {
	case "write_file":
		row.Kind = toolKindWrite
		row.Action = "Write"
		row.Target = displayPath(jsonString(args["path"]))
		if row.Added == 0 && row.Removed == 0 {
			if content := jsonString(args["content"]); content != "" {
				row.Added = countLines(content)
			}
		}
	case "edit_file":
		row.Kind = toolKindEdit
		row.Action = "Edited"
		row.Target = displayPath(jsonString(args["path"]))
	case "read_file":
		row.Kind = toolKindRead
		row.Action = "Read"
		row.Target = displayPathFull(jsonString(args["path"]))
		if row.Output != "" {
			row.Lines = countLines(row.Output)
		}
	case "bash":
		row.Kind = toolKindBash
		row.Action = "Bash"
		row.Target = oneLine(jsonString(args["command"]))
	case "browser":
		row.Kind = toolKindWeb
		row.Action = jsonString(args["action"])
		if row.Action == "" {
			row.Action = "browser"
		}
		if u := jsonString(args["url"]); u != "" {
			row.Target = u
		} else if sel := jsonString(args["selector"]); sel != "" {
			row.Target = sel
		} else if tid := jsonString(args["tabId"]); tid != "" {
			row.Target = tid
		}
	case "web_search":
		row.Kind = toolKindWeb
		row.Action = "Search"
		row.Target = oneLine(jsonString(args["query"]))
	case "web_fetch":
		row.Kind = toolKindWeb
		row.Action = "Fetch"
		if u := jsonString(args["url"]); u != "" {
			row.Target = oneLine(u)
		} else if raw, ok := args["urls"]; ok {
			var urls []string
			_ = json.Unmarshal(raw, &urls)
			if len(urls) > 0 {
				row.Target = oneLine(urls[0])
				if len(urls) > 1 {
					row.Target += fmt.Sprintf(" +%d", len(urls)-1)
				}
			}
		}
	case "list_dir":
		row.Kind = toolKindOther
		row.Action = "List"
		row.Target = displayPath(jsonString(args["path"]))
		if row.Target == "" {
			row.Target = "."
		}
	case "glob":
		row.Kind = toolKindOther
		row.Action = "Glob"
		row.Target = oneLine(jsonString(args["pattern"]))
	case "grep":
		row.Kind = toolKindOther
		row.Action = "Grep"
		row.Target = truncateMiddle(oneLine(jsonString(args["pattern"])), 56)
	case "agent_swarm":
		row.Kind = toolKindSwarm
		row.Action = "Spawned"
		if goal := jsonString(args["goal"]); goal != "" {
			row.Target = oneLine(goal)
		}
	case "skills_list":
		row.Kind = toolKindOther
		row.Action = "skills_list"
		if cat := jsonString(args["category"]); cat != "" {
			row.Target = "[" + cat + "]"
		} else {
			row.Target = "all categories"
		}
	case "skill_view":
		row.Kind = toolKindOther
		row.Action = "skill_view"
		row.Target = jsonString(args["name"])
		if fp := jsonString(args["file_path"]); fp != "" {
			row.Target += " (" + fp + ")"
		}
	case "memory":
		row.Kind = toolKindOther
		row.Action = "memory"
		act := jsonString(args["action"])
		tgt := jsonString(args["target"])
		if tgt == "" {
			tgt = "memory"
		}
		if act != "" {
			row.Target = act + " → " + tgt
			switch act {
			case "add":
				if content := oneLine(jsonString(args["content"])); content != "" {
					row.Target += ": " + truncateMiddle(content, 56)
				}
			case "replace":
				if repl := oneLine(jsonString(args["replacement"])); repl != "" {
					row.Target += ": " + truncateMiddle(repl, 56)
				}
			case "remove":
				if match := oneLine(jsonString(args["match"])); match != "" {
					row.Target += ": " + truncateMiddle(match, 56)
				}
			}
		}
	case "skill_manage":
		row.Kind = toolKindOther
		row.Action = "skill_manage"
		row.Target = jsonString(args["action"]) + " " + jsonString(args["name"])
	default:
		row.Kind = toolKindOther
		row.Action = toolActionLabel(name)
		row.Target = truncateMiddle(oneLine(msg.toolArgs), 56)
	}

	if row.Target == "" {
		row.Target = name
	}
	return row
}

func displayPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	path = filepath.ToSlash(path)
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, filepath.ToSlash(home)) {
		return "~" + strings.TrimPrefix(path, filepath.ToSlash(home))
	}
	return path
}

func displayPathFull(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if !filepath.IsAbs(path) {
		if cwd, err := os.Getwd(); err == nil {
			path = filepath.Join(cwd, path)
		}
	}
	path = filepath.ToSlash(path)
	home, err := os.UserHomeDir()
	if err == nil {
		home = filepath.ToSlash(home)
		if strings.HasPrefix(path, home) {
			return "~" + strings.TrimPrefix(path, home)
		}
	}
	return path
}

type swarmTaskArg struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

func parseAgentSwarmArgs(argsJSON string) (goal, sharedContext string, tasks []swarmTaskArg) {
	if argsJSON == "" {
		return "", "", nil
	}
	var raw struct {
		Goal          string         `json:"goal"`
		SharedContext string         `json:"shared_context"`
		Tasks         []swarmTaskArg `json:"tasks"`
	}
	if json.Unmarshal([]byte(argsJSON), &raw) != nil {
		return "", "", nil
	}
	for _, t := range raw.Tasks {
		if strings.TrimSpace(t.Prompt) == "" {
			continue
		}
		tasks = append(tasks, swarmTaskArg{
			ID:     strings.TrimSpace(t.ID),
			Prompt: strings.TrimSpace(t.Prompt),
		})
	}
	return strings.TrimSpace(raw.Goal), strings.TrimSpace(raw.SharedContext), tasks
}

func swarmAgentLabel(task swarmTaskArg, index int) string {
	if task.ID != "" {
		return task.ID
	}
	return fmt.Sprintf("agent-%d", index+1)
}

func renderSpawnHeader(styles Styles, id, role, status string, attempts int, animating bool, spinnerFrame int) string {
	rolePart := ""
	if role != "" {
		rolePart = " " + styles.ToolMuted.Render("["+role+"]")
	}
	statusPart := ""
	if status != "" && !animating {
		statusText := "(" + status + ")"
		switch status {
		case "error", "aborted":
			statusPart = " " + styles.AssistError.Render(statusText)
		case "ok":
			statusPart = " " + styles.LogOk.Render(statusText)
		default:
			statusPart = " " + styles.ToolMuted.Render(statusText)
		}
	}
	retryPart := ""
	if attempts > 1 && !animating {
		retryPart = " " + styles.ToolMuted.Render(fmt.Sprintf("×%d", attempts))
	}
	head := styles.ToolAction.Render("Spawned") + " " +
		styles.LogAccent.Render(id) + rolePart + statusPart + retryPart
	return lipgloss.JoinHorizontal(lipgloss.Bottom, spawnBullet(styles, animating, spinnerFrame), " ", head)
}

type swarmWorkerInfo struct {
	Status   string
	Attempts int
	Error    string
}

func parseSwarmWorkerInfo(output string) map[string]swarmWorkerInfo {
	info := make(map[string]swarmWorkerInfo)
	var current string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "## ") {
			if current != "" && strings.HasPrefix(line, "Error:") {
				entry := info[current]
				entry.Error = strings.TrimSpace(strings.TrimPrefix(line, "Error:"))
				info[current] = entry
			}
			continue
		}
		rest := strings.TrimPrefix(line, "## ")
		id, rest, ok := strings.Cut(rest, " [")
		if !ok {
			continue
		}
		st, _, ok := strings.Cut(rest, "]")
		if !ok {
			continue
		}
		current = strings.TrimSpace(id)
		entry := swarmWorkerInfo{Status: strings.TrimSpace(st), Attempts: 1}
		if ix := strings.Index(rest, "×"); ix >= 0 {
			n := 0
			for _, r := range rest[ix+len("×"):] {
				if r < '0' || r > '9' {
					break
				}
				n = n*10 + int(r-'0')
			}
			if n > 1 {
				entry.Attempts = n
			}
		}
		info[current] = entry
	}
	return info
}

func renderAgentSwarmBlock(styles Styles, row toolRow, width int, expanded bool, spinnerFrame int) []string {
	goal, sharedContext, tasks := parseAgentSwarmArgs(row.Args)
	infoByID := parseSwarmWorkerInfo(row.Output)
	indent := "  "
	ctxIndent := "    "

	var lines []string

	renderOne := func(id, role, prompt string, info swarmWorkerInfo, animating bool) {
		lines = append(lines, renderSpawnHeader(styles, id, role, info.Status, info.Attempts, animating, spinnerFrame))
		taskLine := indent + "└ " + term.TruncateWidth(oneLine(prompt), width-4)
		lines = append(lines, styles.ToolSub.Render(taskLine))
		if expanded && info.Error != "" {
			lines = append(lines, styles.AssistError.Render(ctxIndent+"Error: "+info.Error))
		}
		if sharedContext != "" {
			lines = append(lines, styles.ToolSub.Render(ctxIndent+"Context:"))
			ctx := sharedContext
			if !expanded {
				ctx = term.TruncateWidth(oneLine(ctx), width-6)
			}
			lines = append(lines, styles.ToolOutput.Render(ctxIndent+"- "+ctx))
		}
	}

	switch {
	case len(tasks) > 0:
		for i, task := range tasks {
			id := swarmAgentLabel(task, i)
			info := infoByID[id]
			if row.Pending && info.Status == "" {
				info.Status = "running…"
			}
			animating := row.Pending && (info.Status == "" || info.Status == "running…" || info.Status == "planning…")
			renderOne(id, "worker", task.Prompt, info, animating)
		}
	case goal != "":
		info := swarmWorkerInfo{}
		animating := row.Pending
		if row.Pending {
			info.Status = "planning…"
		} else if row.Output != "" {
			info.Status = "done"
		}
		renderOne("swarm", "planner", goal, info, animating)
	default:
		lines = append(lines, renderSpawnHeader(styles, "swarm", "worker", "", 1, row.Pending, spinnerFrame))
		lines = append(lines, styles.ToolSub.Render(indent+"└ agents"))
	}

	if row.Pending && row.Output != "" {
		progress := strings.TrimSpace(strings.Split(row.Output, "\n")[0])
		if progress != "" {
			lines = append(lines, styles.ToolPending.Render(ctxIndent+progress))
		}
	}

	if row.Error {
		lines = append(lines, styles.AssistError.Render(ctxIndent+"failed"))
	}

	return lines
}

func renderWebSearchBlock(styles Styles, row toolRow, width int, expanded bool) []string {
	query := row.Target
	if query == "" {
		query = "query"
	}
	header := styles.AssistBullet.Render("*") + " " +
		styles.ToolAction.Render("Search") + " " +
		styles.LogAccent.Render(term.TruncateWidth(query, width-12))

	lines := []string{header}
	taskLine := "  └ " + term.TruncateWidth(oneLine(query), width-4)
	lines = append(lines, styles.ToolSub.Render(taskLine))

	if row.Pending {
		lines = append(lines, styles.ToolPending.Render("    searching…"))
		return lines
	}

	out := strings.TrimSpace(row.Output)
	if out == "" {
		return lines
	}

	if !row.Error {
		if n := strings.Count(out, "\n\n"); n > 0 {
			lines = append(lines, styles.ToolMuted.Render(fmt.Sprintf("    %d results", n+1)))
		}
	}

	if expanded {
		detail := limitToolOutput(out, true)
		for i, line := range strings.Split(detail, "\n") {
			if line == "" {
				continue
			}
			prefix := "    "
			if i == 0 {
				prefix = "    "
			}
			lines = append(lines, styles.ToolOutput.Render(prefix+line))
		}
	}

	return lines
}

func toolActionLabel(name string) string {
	parts := strings.Fields(strings.ReplaceAll(name, "_", " "))
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(strings.TrimRight(s, "\n"), "\n"))
}

func renderToolGroup(styles Styles, tools []chatMsg, width int, expanded bool, spinnerFrame int) string {
	if len(tools) == 0 {
		return ""
	}

	rows := make([]toolRow, len(tools))
	for i, msg := range tools {
		rows[i] = parseToolRow(msg)
	}

	var lines []string

	if len(tools) > 1 {
		header := fmt.Sprintf("Updated  %d items", len(tools))
		lines = append(lines, styles.ToolMuted.Render(header))
	}

	for i, msg := range tools {
		row := rows[i]
		lines = append(lines, renderToolBlock(styles, row, width, expanded, spinnerFrame)...)

		if expanded && row.Output != "" && !row.Pending &&
			row.Kind != toolKindBash && row.Kind != toolKindSwarm && row.Kind != toolKindWeb {
			detail := limitToolOutput(row.Output, true)
			outStyle := styles.ToolOutput
			if row.Error {
				outStyle = styles.AssistError
			}
			if row.Kind == toolKindRead && strings.Contains(row.Output, "Read image file [") {
				for j, line := range strings.Split(detail, "\n") {
					prefix := "  "
					if j == 0 {
						prefix = "└ "
					}
					lines = append(lines, outStyle.Render(prefix+line))
				}

				var args map[string]json.RawMessage
				_ = json.Unmarshal([]byte(row.Args), &args)
				pathStr := jsonString(args["path"])
				if pathStr != "" {
					absPath := pathStr
					if !filepath.IsAbs(absPath) {
						if cwd, err := os.Getwd(); err == nil {
							absPath = filepath.Join(cwd, absPath)
						}
					}
					fileURL := "file://" + filepath.ToSlash(filepath.Clean(absPath))
					renderedMD := markdown.RenderAttachmentImage(fileURL, width, assistantMarkdownTheme(styles), markdown.RenderOptions{})
					for _, line := range strings.Split(renderedMD, "\n") {
						lines = append(lines, line)
					}
				}
			} else {
				for j, line := range strings.Split(detail, "\n") {
					prefix := "  "
					if j == 0 {
						prefix = "└ "
					}
					lines = append(lines, outStyle.Render(prefix+line))
				}
			}
		}
		_ = msg
	}

	return strings.Join(lines, "\n")
}

func renderToolBlock(styles Styles, row toolRow, width int, expanded bool, spinnerFrame int) []string {
	if row.Name == "browser" {
		return renderBrowserBlock(styles, row, width, expanded)
	}
	switch row.Kind {
	case toolKindWrite:
		return []string{renderWriteLine(styles, row)}
	case toolKindEdit:
		return []string{renderEditLine(styles, row)}
	case toolKindRead:
		return renderReadBlock(styles, row)
	case toolKindBash:
		return renderBashBlock(styles, row, width, expanded)
	case toolKindSwarm:
		return renderAgentSwarmBlock(styles, row, width, expanded, spinnerFrame)
	case toolKindWeb:
		return renderWebSearchBlock(styles, row, width, expanded)
	default:
		switch row.Name {
		case "skills_list":
			return renderSkillsListBlock(styles, row, expanded)
		case "skill_view":
			return renderSkillViewBlock(styles, row, expanded)
		case "skill_manage":
			return renderSkillManageBlock(styles, row, expanded)
		case "memory":
			return renderMemoryBlock(styles, row, expanded)
		}
		return []string{renderGenericLine(styles, row)}
	}
}

func renderWriteLine(styles Styles, row toolRow) string {
	head := styles.ToolMuted.Render("Write " + row.Target)
	if row.Pending {
		return head + " " + styles.ToolPending.Render("…")
	}
	if row.Added == 0 && row.Removed == 0 {
		return head + " " + styles.ToolMuted.Render(">")
	}
	delta := styles.ToolDelta.Render(fmt.Sprintf("+%d", row.Added)) +
		styles.ToolDeltaRemoved.Render(fmt.Sprintf("-%d", row.Removed))
	return head + " " + delta + " " + styles.ToolMuted.Render(">")
}

func renderEditLine(styles Styles, row toolRow) string {
	head := styles.ToolMuted.Render("Edited " + row.Target)
	if row.Pending {
		return head + " " + styles.ToolPending.Render("…")
	}
	if row.Added == 0 && row.Removed == 0 {
		return head + " " + styles.ToolMuted.Render(">")
	}
	delta := styles.ToolDelta.Render(fmt.Sprintf("+%d", row.Added)) +
		styles.ToolDeltaRemoved.Render(fmt.Sprintf("-%d", row.Removed))
	return head + " " + delta + " " + styles.ToolMuted.Render(">")
}

func renderReadBlock(styles Styles, row toolRow) []string {
	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render("Read") + " " +
		styles.ToolTarget.Render(row.Target)

	lines := []string{header}
	switch {
	case row.Pending:
		lines = append(lines, styles.ToolSub.Render("└ …"))
	case strings.Contains(row.Output, "Read image file ["):
		var args map[string]json.RawMessage
		_ = json.Unmarshal([]byte(row.Args), &args)
		path := jsonString(args["path"])
		lines = append(lines, styles.ToolSub.Render(parseImageInfo(row.Output, path)))
	case row.Lines > 0:
		lines = append(lines, styles.ToolSub.Render(fmt.Sprintf("└ Read %d lines", row.Lines)))
	}
	return lines
}

func parseImageInfo(output, path string) string {
	lines := strings.Split(output, "\n")
	filename := filepath.Base(path)
	if filename == "." || filename == "/" || filename == "" {
		filename = "image"
	}
	if len(lines) > 1 {
		secondLine := strings.TrimSpace(lines[1])
		if secondLine != "" && !strings.HasPrefix(secondLine, "[") {
			return fmt.Sprintf("└ Read image %s (%s)", filename, secondLine)
		}
	}
	return fmt.Sprintf("└ Read image %s", filename)
}

func renderBashBlock(styles Styles, row toolRow, width int, expanded bool) []string {
	cmd := row.Target
	if cmd == "" {
		cmd = "command"
	}
	cmd = term.TruncateWidth(cmd, width-12)

	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render("Bash") + " " +
		styles.ToolTarget.Render(cmd)

	lines := []string{header}

	out := strings.TrimRight(row.Output, "\n")
	if out == "" {
		if row.Pending {
			lines = append(lines, styles.ToolPending.Render("└ running… (esc to cancel)"))
		}
		return lines
	}

	outStyle := styles.ToolOutput
	if row.Error {
		outStyle = styles.AssistError
	}

	// While streaming and collapsed, show the tail so the newest output is
	// visible; once finished, show the head with a "more lines" hint.
	var detail string
	if row.Pending && !expanded {
		detail = tailLines(out, 8)
	} else {
		detail = limitToolOutput(out, expanded)
	}

	dl := strings.Split(detail, "\n")
	for i, line := range dl {
		if line == "" && i == len(dl)-1 {
			continue
		}
		prefix := "  "
		if i == 0 {
			prefix = "└ "
		}
		lines = append(lines, outStyle.Render(prefix+line))
	}

	if row.Pending {
		lines = append(lines, styles.ToolPending.Render("  running… (esc to cancel)"))
	}
	return lines
}

// tailLines returns the last n non-trailing-empty lines of text, prefixed with
// an elision hint when earlier lines were dropped.
func tailLines(text string, n int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return text
	}
	tail := lines[len(lines)-n:]
	return fmt.Sprintf("… (%d earlier lines)\n", len(lines)-n) + strings.Join(tail, "\n")
}

func renderGenericLine(styles Styles, row toolRow) string {
	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render(row.Action) + " " +
		styles.ToolTarget.Render(row.Target)
	if row.Pending {
		return header + " " + styles.ToolPending.Render("…")
	}
	return header
}

func renderSkillsListBlock(styles Styles, row toolRow, expanded bool) []string {
	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render("skills_list")
	if row.Target != "all categories" {
		header += " " + styles.ToolTarget.Render(row.Target)
	}
	lines := []string{header}

	if row.Pending {
		lines = append(lines, styles.ToolSub.Render("└ …"))
		return lines
	}

	if row.Error {
		lines = append(lines, styles.AssistError.Render("└ failed"))
		return lines
	}

	var result struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(row.Output), &result); err == nil {
		lines = append(lines, styles.ToolSub.Render(fmt.Sprintf("└ %d skills", result.Count)))
	} else {
		lines = append(lines, styles.ToolSub.Render("└ completed"))
	}
	return lines
}

func renderSkillViewBlock(styles Styles, row toolRow, expanded bool) []string {
	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render("skill_view") + " " +
		styles.ToolTarget.Render(row.Target)
	lines := []string{header}

	if row.Pending {
		lines = append(lines, styles.ToolSub.Render("└ …"))
		return lines
	}

	if row.Error {
		lines = append(lines, styles.AssistError.Render("└ failed"))
		return lines
	}

	var result struct {
		Name string `json:"name"`
		File string `json:"file"`
	}
	if err := json.Unmarshal([]byte(row.Output), &result); err == nil {
		if result.File != "" {
			lines = append(lines, styles.ToolSub.Render(fmt.Sprintf("└ loaded skill '%s' file '%s'", result.Name, result.File)))
		} else {
			lines = append(lines, styles.ToolSub.Render(fmt.Sprintf("└ loaded skill '%s'", result.Name)))
		}
	} else {
		lines = append(lines, styles.ToolSub.Render("└ completed"))
	}
	return lines
}

func renderSkillManageBlock(styles Styles, row toolRow, expanded bool) []string {
	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render("skill_manage") + " " +
		styles.ToolTarget.Render(row.Target)
	lines := []string{header}

	if row.Pending {
		lines = append(lines, styles.ToolSub.Render("└ …"))
		return lines
	}

	if row.Error {
		lines = append(lines, styles.AssistError.Render("└ failed"))
		return lines
	}

	var result struct {
		Success   bool   `json:"success"`
		Staged    bool   `json:"staged"`
		PendingID string `json:"pending_id"`
		Message   string `json:"message"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal([]byte(row.Output), &result); err == nil {
		if result.Success {
			msg := result.Message
			if msg == "" {
				msg = "success"
			}
			subStyle := styles.ToolSub
			if result.Staged {
				subStyle = styles.FooterWarn
				if result.PendingID != "" {
					msg = msg + " (id: " + result.PendingID + ")"
				}
			}
			lines = append(lines, subStyle.Render("└ "+msg))
		} else {
			errMsg := result.Error
			if errMsg == "" {
				errMsg = "failed"
			}
			lines = append(lines, styles.AssistError.Render("└ "+errMsg))
		}
	} else {
		lines = append(lines, styles.ToolSub.Render("└ completed"))
	}
	return lines
}

func renderMemoryBlock(styles Styles, row toolRow, expanded bool) []string {
	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render("memory") + " " +
		styles.ToolTarget.Render(row.Target)
	lines := []string{header}

	if row.Pending {
		lines = append(lines, styles.ToolSub.Render("└ …"))
		return lines
	}
	if row.Error {
		lines = append(lines, styles.AssistError.Render("└ failed"))
		return lines
	}

	var result struct {
		Success   bool   `json:"success"`
		Staged    bool   `json:"staged"`
		PendingID string `json:"pending_id"`
		Message   string `json:"message"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal([]byte(row.Output), &result); err == nil {
		if result.Success {
			msg := result.Message
			if msg == "" {
				msg = "saved"
			}
			subStyle := styles.ToolSub
			if result.Staged {
				subStyle = styles.FooterWarn
				if result.PendingID != "" {
					msg = msg + " (id: " + result.PendingID + ")"
				}
			}
			lines = append(lines, subStyle.Render("└ "+msg))
		} else {
			errMsg := result.Error
			if errMsg == "" {
				errMsg = "failed"
			}
			lines = append(lines, styles.AssistError.Render("└ "+errMsg))
		}
	} else {
		lines = append(lines, styles.ToolSub.Render("└ completed"))
	}
	return lines
}

func formatToolCall(name, argsJSON string) string {
	row := parseToolRow(chatMsg{toolName: name, toolArgs: argsJSON})
	if row.Target != "" && row.Action != "" {
		return row.Action + " " + row.Target
	}
	return name
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateMiddle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	head := max/2 - 1
	tail := max - head - 1
	return s[:head] + "…" + s[len(s)-tail:]
}

func limitToolOutput(text string, expanded bool) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	if expanded {
		return text
	}
	const maxLines = 8
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n… (%d more lines)", len(lines)-maxLines)
}

func renderBrowserBlock(styles Styles, row toolRow, width int, expanded bool) []string {
	action := row.Action
	target := row.Target
	header := styles.ToolBullet.Render("●") + " " +
		styles.ToolAction.Render("browser") + " " +
		styles.LogAccent.Render(action) + " " +
		styles.ToolTarget.Render(term.TruncateWidth(target, width-20))

	lines := []string{header}

	if row.Pending {
		lines = append(lines, styles.ToolPending.Render("  Running browser action..."))
		return lines
	}

	if row.Error {
		msg := row.Output
		if len(msg) > 120 {
			msg = msg[:120]
		}
		lines = append(lines, styles.AssistError.Render("  "+msg))
		return lines
	}

	if action == "list" {
		tabCount := 0
		if row.Metadata != "" {
			var details struct {
				Tabs []interface{} `json:"tabs"`
			}
			if err := json.Unmarshal([]byte(row.Metadata), &details); err == nil {
				tabCount = len(details.Tabs)
			}
		} else if row.Output != "" && row.Output != "No tabs." {
			tabCount = len(strings.Split(strings.TrimSpace(row.Output), "\n"))
		}
		lines = append(lines, styles.ToolOutput.Render(fmt.Sprintf("  %d tab(s)", tabCount)))
	} else {
		var args map[string]json.RawMessage
		_ = json.Unmarshal([]byte(row.Args), &args)
		tabID := jsonString(args["tabId"])

		lbl := action
		if tabID != "" {
			lbl += " " + tabID
		}
		lines = append(lines, styles.ToolOutput.Render("  "+lbl))
	}

	if expanded && row.Output != "" {
		detail := limitToolOutput(row.Output, true)
		for _, line := range strings.Split(detail, "\n") {
			lines = append(lines, styles.ToolOutput.Render("  "+line))
		}
	}

	return lines
}
