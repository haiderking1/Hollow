package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/session"
)

type FlatTreeNode struct {
	ID          string
	DisplayText string
	Indent      int
	IsActive    bool
}

// buildFlatTreeNodes recursively traverses session tree nodes to build a flat list for TUI render.
func (a *App) buildFlatTreeNodes(roots []*session.SessionTreeNode, indent int, activeID *string) []FlatTreeNode {
	var out []FlatTreeNode
	for _, root := range roots {
		isActive := activeID != nil && root.Entry.ID == *activeID

		// Get display text
		var label string
		if root.Label != "" {
			label = root.Label
		}
		displayText := getEntryDisplayText(root.Entry, label)

		out = append(out, FlatTreeNode{
			ID:          root.Entry.ID,
			DisplayText: displayText,
			Indent:      indent,
			IsActive:    isActive,
		})

		// Recursively append children with increased indentation
		out = append(out, a.buildFlatTreeNodes(root.Children, indent+1, activeID)...)
	}
	return out
}

func getEntryDisplayText(e session.FileEntry, label string) string {
	var suffix string
	if label != "" {
		suffix = " (" + label + ")"
	}
	switch e.Type {
	case session.TypeSession:
		return "[Session ID: " + e.ID + "]" + suffix
	case session.TypeMessage:
		if e.Message == nil {
			return "[Message: empty]" + suffix
		}
		role := e.Message.Role
		content := ""
		if e.Message.Content != nil {
			content = *e.Message.Content
		}
		if len(content) > 60 {
			content = content[:57] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		return fmt.Sprintf("[%s]: %s", role, content) + suffix
	case session.TypeCompaction:
		summary := e.Summary
		if len(summary) > 60 {
			summary = summary[:57] + "..."
		}
		summary = strings.ReplaceAll(summary, "\n", " ")
		return fmt.Sprintf("[Compacted]: %s", summary) + suffix
	case session.TypeBranchSummary:
		summary := e.Summary
		if len(summary) > 60 {
			summary = summary[:57] + "..."
		}
		summary = strings.ReplaceAll(summary, "\n", " ")
		return fmt.Sprintf("[Branch Summary]: %s", summary) + suffix
	case session.TypeCustomMessage:
		var text string
		if s, ok := e.Content.(string); ok {
			text = s
		}
		if len(text) > 60 {
			text = text[:57] + "..."
		}
		text = strings.ReplaceAll(text, "\n", " ")
		return fmt.Sprintf("[Custom]: %s", text) + suffix
	default:
		return fmt.Sprintf("[%s]", e.Type) + suffix
	}
}

func (a *App) handleTreePickerKey(k parsedKey) bool {
	switch k.action {
	case keyUp:
		if a.treePickerConfirm == 1 {
			if a.treePickerChoice > 0 {
				a.treePickerChoice--
			}
		} else {
			if a.treePickerCursor > 0 {
				a.treePickerCursor--
			}
		}
		a.requestRender()
		return true
	case keyDown:
		if a.treePickerConfirm == 1 {
			if a.treePickerChoice < 2 {
				a.treePickerChoice++
			}
		} else {
			a.treePickerCursor++
			a.clampTreePickerCursor()
		}
		a.requestRender()
		return true
	case keyRune:
		if k.r == 'k' || k.r == 'K' {
			if a.treePickerConfirm == 1 {
				if a.treePickerChoice > 0 {
					a.treePickerChoice--
				}
			} else {
				if a.treePickerCursor > 0 {
					a.treePickerCursor--
				}
			}
			a.requestRender()
			return true
		}
		if k.r == 'j' || k.r == 'J' {
			if a.treePickerConfirm == 1 {
				if a.treePickerChoice < 2 {
					a.treePickerChoice++
				}
			} else {
				a.treePickerCursor++
				a.clampTreePickerCursor()
			}
			a.requestRender()
			return true
		}
	case keyEscape:
		if a.treePickerConfirm == 1 {
			a.treePickerConfirm = 0 // go back to picking node
		} else {
			a.dismissTreePicker()
		}
		a.requestRender()
		return true
	case keyEnter:
		if a.treePickerConfirm == 0 {
			if a.treePickerCursor >= 0 && a.treePickerCursor < len(a.treePickerNodes) {
				node := a.treePickerNodes[a.treePickerCursor]
				a.treePickerTarget = node.ID
				a.treePickerConfirm = 1
				a.treePickerChoice = 0 // default: "No summary"
			}
		} else if a.treePickerConfirm == 1 {
			targetID := a.treePickerTarget
			wantsSummary := a.treePickerChoice != 0
			customInstructions := ""

			a.dismissTreePicker()
			a.requestRender()

			// Setup async run
			a.running = true

			a.runAgentTask(func(emit func(core.Event)) {
				cfg, err := config.LoadRuntime()
				if err != nil {
					emit(core.Event{Kind: core.EventError, Data: err.Error()})
					return
				}
				ag := a.ensureAgent(cfg)
				ag.SetEmit(emit)

				_, _ = ag.NavigateToEntry(context.Background(), targetID, agent.NavigateOptions{
					Summarize:          wantsSummary,
					CustomInstructions: customInstructions,
				})
			})
		}
		a.requestRender()
		return true
	}
	return false
}

func (a *App) clampTreePickerCursor() {
	if len(a.treePickerNodes) == 0 {
		a.treePickerCursor = 0
		return
	}
	if a.treePickerCursor >= len(a.treePickerNodes) {
		a.treePickerCursor = len(a.treePickerNodes) - 1
	}
	if a.treePickerCursor < 0 {
		a.treePickerCursor = 0
	}
}

func (a *App) dismissTreePicker() {
	a.mode = modeTask
	a.treePickerNodes = nil
	a.treePickerCursor = 0
	a.treePickerConfirm = 0
	a.treePickerChoice = 0
	a.treePickerTarget = ""
}
