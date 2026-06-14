package session

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/opencode"
)

const (
	CompactionSummaryPrefix = "The conversation history before this point was compacted into the following summary:\n\n<summary>\n"
	CompactionSummarySuffix = "\n</summary>"
	BranchSummaryPrefix     = "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n"
	BranchSummarySuffix     = "\n</summary>"
)

type FileOperations struct {
	Read    map[string]bool
	Written map[string]bool
	Edited  map[string]bool
}

func NewFileOps() FileOperations {
	return FileOperations{
		Read:    make(map[string]bool),
		Written: make(map[string]bool),
		Edited:  make(map[string]bool),
	}
}

type CompactionDetails struct {
	ReadFiles     []string `json:"readFiles"`
	ModifiedFiles []string `json:"modifiedFiles"`
}

func ExtractFileOpsFromMessage(msg opencode.Message, fileOps FileOperations) {
	if msg.Role != "assistant" {
		return
	}
	for _, tc := range msg.ToolCalls {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			continue
		}
		pathVal, ok := args["path"]
		if !ok {
			continue
		}
		path, ok := pathVal.(string)
		if !ok || path == "" {
			continue
		}
		switch tc.Function.Name {
		case "read_file":
			fileOps.Read[path] = true
		case "write_file":
			fileOps.Written[path] = true
		case "edit_file":
			fileOps.Edited[path] = true
		}
	}
}

func ComputeFileLists(fileOps FileOperations) ([]string, []string) {
	modified := make(map[string]bool)
	for f := range fileOps.Edited {
		modified[f] = true
	}
	for f := range fileOps.Written {
		modified[f] = true
	}

	var readOnly []string
	for f := range fileOps.Read {
		if !modified[f] {
			readOnly = append(readOnly, f)
		}
	}
	sort.Strings(readOnly)

	var modifiedFiles []string
	for f := range modified {
		modifiedFiles = append(modifiedFiles, f)
	}
	sort.Strings(modifiedFiles)

	return readOnly, modifiedFiles
}

func FormatFileOperations(readFiles []string, modifiedFiles []string) string {
	var sections []string
	if len(readFiles) > 0 {
		sections = append(sections, fmt.Sprintf("<read-files>\n%s\n</read-files>", strings.Join(readFiles, "\n")))
	}
	if len(modifiedFiles) > 0 {
		sections = append(sections, fmt.Sprintf("<modified-files>\n%s\n</modified-files>", strings.Join(modifiedFiles, "\n")))
	}
	if len(sections) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sections, "\n\n")
}

func truncateForSummary(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	truncatedChars := len(text) - maxChars
	return fmt.Sprintf("%s\n\n[... %d more characters truncated]", text[:maxChars], truncatedChars)
}

func SerializeConversation(messages []opencode.Message) string {
	var parts []string
	for _, msg := range messages {
		if msg.Role == "user" {
			content := opencode.ContentString(msg)
			if content != "" {
				parts = append(parts, fmt.Sprintf("[User]: %s", content))
			}
		} else if msg.Role == "assistant" {
			var textParts []string
			var thinkingParts []string
			var toolCalls []string

			if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
				thinkingParts = append(thinkingParts, *msg.ReasoningContent)
			}
			content := opencode.ContentString(msg)
			if content != "" {
				textParts = append(textParts, content)
			}
			for _, tc := range msg.ToolCalls {
				var args map[string]any
				var argsStr string
				if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
					var kv []string
					// Sort keys for deterministic serialization in tests
					var keys []string
					for k := range args {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						v := args[k]
						vBytes, _ := json.Marshal(v)
						kv = append(kv, fmt.Sprintf("%s=%s", k, string(vBytes)))
					}
					argsStr = strings.Join(kv, ", ")
				} else {
					argsStr = tc.Function.Arguments
				}
				toolCalls = append(toolCalls, fmt.Sprintf("%s(%s)", tc.Function.Name, argsStr))
			}

			if len(thinkingParts) > 0 {
				parts = append(parts, fmt.Sprintf("[Assistant thinking]: %s", strings.Join(thinkingParts, "\n")))
			}
			if len(textParts) > 0 {
				parts = append(parts, fmt.Sprintf("[Assistant]: %s", strings.Join(textParts, "\n")))
			}
			if len(toolCalls) > 0 {
				parts = append(parts, fmt.Sprintf("[Assistant tool calls]: %s", strings.Join(toolCalls, "; ")))
			}
		} else if msg.Role == "tool" {
			content := opencode.ContentString(msg)
			if content != "" {
				parts = append(parts, fmt.Sprintf("[Tool result]: %s", truncateForSummary(content, 2000)))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func ConvertToLlm(messages []opencode.Message) []opencode.Message {
	var out []opencode.Message
	for _, m := range messages {
		switch m.Role {
		case "compactionSummary":
			content := CompactionSummaryPrefix + opencode.ContentString(m) + CompactionSummarySuffix
			out = append(out, opencode.Message{
				Role:    "user",
				Content: opencode.StringContent(content),
			})
		case "branchSummary":
			content := BranchSummaryPrefix + opencode.ContentString(m) + BranchSummarySuffix
			out = append(out, opencode.Message{
				Role:    "user",
				Content: opencode.StringContent(content),
			})
		default:
			out = append(out, m)
		}
	}
	return out
}

func CalculateContextTokens(usage opencode.Usage) int {
	if usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	return usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
}

func GetLastAssistantUsage(entries []FileEntry) *opencode.Usage {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == TypeMessage && entries[i].Message != nil && entries[i].Message.Role == "assistant" {
			if entries[i].Message.Usage != nil {
				return entries[i].Message.Usage
			}
		}
	}
	return nil
}

type ContextUsageEstimate struct {
	Tokens         int
	UsageTokens    int
	TrailingTokens int
	LastUsageIndex int
}

func EstimateMessageTokens(msg opencode.Message) int {
	chars := 0
	if msg.Role == "user" {
		if len(msg.Content) > 0 {
			chars = len(opencode.ContentString(msg))
		}
		return (chars + 3) / 4
	}
	if msg.Role == "assistant" {
		if len(msg.Content) > 0 {
			chars += len(opencode.ContentString(msg))
		}
		if msg.ReasoningContent != nil {
			chars += len(*msg.ReasoningContent)
		}
		for _, tc := range msg.ToolCalls {
			chars += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
		return (chars + 3) / 4
	}
	if msg.Role == "tool" || msg.Role == "toolResult" {
		if len(msg.Content) > 0 {
			chars = len(opencode.ContentString(msg))
		}
		return (chars + 3) / 4
	}
	if msg.Role == "compactionSummary" || msg.Role == "branchSummary" {
		if len(msg.Content) > 0 {
			chars = len(opencode.ContentString(msg))
		}
		return (chars + 3) / 4
	}
	return 0
}

func EstimateTokens(entry FileEntry) int {
	if entry.Type == TypeCompaction || entry.Type == TypeBranchSummary {
		return (len(entry.Summary) + 3) / 4
	}
	if entry.Type != TypeMessage || entry.Message == nil {
		return 0
	}
	return EstimateMessageTokens(*entry.Message)
}

func EstimateContextTokens(messages []opencode.Message) ContextUsageEstimate {
	lastUsageIdx := -1
	var lastUsage *opencode.Usage
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && messages[i].Usage != nil {
			lastUsageIdx = i
			lastUsage = messages[i].Usage
			break
		}
	}

	if lastUsageIdx == -1 {
		estimated := 0
		for _, msg := range messages {
			estimated += EstimateMessageTokens(msg)
		}
		return ContextUsageEstimate{
			Tokens:         estimated,
			UsageTokens:    0,
			TrailingTokens: estimated,
			LastUsageIndex: -1,
		}
	}

	usageTokens := CalculateContextTokens(*lastUsage)
	trailingTokens := 0
	for i := lastUsageIdx + 1; i < len(messages); i++ {
		trailingTokens += EstimateMessageTokens(messages[i])
	}

	return ContextUsageEstimate{
		Tokens:         usageTokens + trailingTokens,
		UsageTokens:    usageTokens,
		TrailingTokens: trailingTokens,
		LastUsageIndex: lastUsageIdx,
	}
}

func ShouldCompact(contextTokens int, contextWindow int, settings config.CompactionSettings) bool {
	if !settings.Enabled {
		return false
	}
	return contextTokens > contextWindow-settings.ReserveTokens
}

func findValidCutPoints(entries []FileEntry, startIndex int, endIndex int) []int {
	var cutPoints []int
	for i := startIndex; i < endIndex; i++ {
		entry := entries[i]
		if entry.Type == TypeMessage && entry.Message != nil {
			role := entry.Message.Role
			if role == "user" || role == "assistant" || role == "bashExecution" || role == "custom" || role == "branchSummary" || role == "compactionSummary" {
				cutPoints = append(cutPoints, i)
			}
		} else if entry.Type == TypeBranchSummary || entry.Type == TypeCustomMessage {
			cutPoints = append(cutPoints, i)
		}
	}
	return cutPoints
}

func findTurnStartIndex(entries []FileEntry, entryIndex int, startIndex int) int {
	for i := entryIndex; i >= startIndex; i-- {
		entry := entries[i]
		if entry.Type == TypeBranchSummary || entry.Type == TypeCustomMessage {
			return i
		}
		if entry.Type == TypeMessage && entry.Message != nil {
			role := entry.Message.Role
			if role == "user" || role == "bashExecution" {
				return i
			}
		}
	}
	return -1
}

type CutPointResult struct {
	FirstKeptEntryIndex int
	TurnStartIndex      int
	IsSplitTurn         bool
}

func FindCutPoint(entries []FileEntry, startIndex int, endIndex int, keepRecentTokens int) CutPointResult {
	cutPoints := findValidCutPoints(entries, startIndex, endIndex)
	if len(cutPoints) == 0 {
		return CutPointResult{FirstKeptEntryIndex: startIndex, TurnStartIndex: -1, IsSplitTurn: false}
	}

	accumulatedTokens := 0
	cutIndex := cutPoints[0]

	for i := endIndex - 1; i >= startIndex; i-- {
		entry := entries[i]
		if entry.Type != TypeMessage {
			continue
		}
		accumulatedTokens += EstimateTokens(entry)
		if accumulatedTokens >= keepRecentTokens {
			for _, cp := range cutPoints {
				if cp >= i {
					cutIndex = cp
					break
				}
			}
			break
		}
	}

	for cutIndex > startIndex {
		prevEntry := entries[cutIndex-1]
		if prevEntry.Type == TypeCompaction || prevEntry.Type == TypeMessage {
			break
		}
		cutIndex--
	}

	cutEntry := entries[cutIndex]
	isUserMessage := cutEntry.Type == TypeMessage && cutEntry.Message != nil && cutEntry.Message.Role == "user"
	turnStartIndex := -1
	if !isUserMessage {
		turnStartIndex = findTurnStartIndex(entries, cutIndex, startIndex)
	}

	return CutPointResult{
		FirstKeptEntryIndex: cutIndex,
		TurnStartIndex:      turnStartIndex,
		IsSplitTurn:         !isUserMessage && turnStartIndex != -1,
	}
}

// GetLatestCompactionEntry walks the branch backwards and returns the latest compaction entry, if any.
func GetLatestCompactionEntry(branch []FileEntry) *FileEntry {
	for i := len(branch) - 1; i >= 0; i-- {
		if branch[i].Type == TypeCompaction {
			return &branch[i]
		}
	}
	return nil
}
