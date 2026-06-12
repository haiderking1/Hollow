package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/session"
	"github.com/enough/enough/backend/skills"
	"github.com/enough/enough/frontend/tui"
)

func main() {
	// 1. Route `enough skills ...` command
	if len(os.Args) >= 2 && os.Args[1] == "skills" {
		runSkillsCLI()
		return
	}

	// 2. Parse command-line args
	var preloads []string
	var query string
	var hasQueryFlag bool

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--skills" {
			if i+1 < len(os.Args) {
				preloads = parseSkillsList(os.Args[i+1])
				i++
			} else {
				fmt.Fprintln(os.Stderr, "Error: --skills option requires an argument")
				os.Exit(1)
			}
		} else if strings.HasPrefix(arg, "--skills=") {
			preloads = parseSkillsList(strings.TrimPrefix(arg, "--skills="))
		} else if arg == "-q" || arg == "--query" {
			hasQueryFlag = true
			if i+1 < len(os.Args) {
				query = os.Args[i+1]
				i++
			} else {
				fmt.Fprintln(os.Stderr, "Error: --query/-q option requires an argument")
				os.Exit(1)
			}
		} else if strings.HasPrefix(arg, "--query=") {
			hasQueryFlag = true
			query = strings.TrimPrefix(arg, "--query=")
		} else if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", arg)
			printUsage()
			os.Exit(1)
		} else {
			// Positional query: join all remaining arguments
			if query == "" {
				query = strings.Join(os.Args[i:], " ")
			}
			break
		}
	}

	if hasQueryFlag && query == "" {
		fmt.Fprintln(os.Stderr, "Error: Query was requested but is empty")
		os.Exit(1)
	}

	// 3. Decide mode
	if query != "" {
		// Single-query mode
		cfg, err := config.LoadRuntime()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}

		sm, err := session.ContinueRecent("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing session: %v\n", err)
			os.Exit(1)
		}

		// Print session ID to stderr so standard outputs are not polluted
		fmt.Fprintf(os.Stderr, "Session ID: %s\n", sm.SessionID())

		if len(preloads) > 0 {
			promptText, loaded, missing, err := skills.BuildPreloadedSkillsPrompt(preloads, sm.CWD(), sm.SessionID(), cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error preloading skills: %v\n", err)
				os.Exit(1)
			}
			if len(missing) > 0 {
				fmt.Fprintf(os.Stderr, "Error: Unknown skill(s): %s\n", strings.Join(missing, ", "))
				os.Exit(1)
			}
			cfg.PreloadedSkillsPrompt = promptText
			cfg.PreloadedSkills = loaded
		}

		ag := agent.New(cfg, "", sm)
		ag.SetNotify(func(msg string) {
			fmt.Fprintf(os.Stderr, "note: %s\n", msg)
		})

		toolDeltas := make(map[string]int)
		var lastThinking string

		emit := func(e core.Event) {
			switch e.Kind {
			case core.EventAssistantThinkingDelta:
				if delta, ok := e.Data.(string); ok {
					if strings.HasPrefix(delta, lastThinking) {
						newPart := strings.TrimPrefix(delta, lastThinking)
						fmt.Fprint(os.Stderr, newPart)
						lastThinking = delta
					} else {
						fmt.Fprint(os.Stderr, delta)
						lastThinking = delta
					}
				}
			case core.EventAssistantDelta:
				if delta, ok := e.Data.(string); ok {
					fmt.Print(delta)
				}
			case core.EventError:
				if text, ok := e.Data.(string); ok {
					fmt.Fprintf(os.Stderr, "error: %s\n", text)
				}
			case core.EventSystem:
				if text, ok := e.Data.(string); ok {
					fmt.Fprintf(os.Stderr, "system: %s\n", text)
				}
			case core.EventToolStart:
				if ev, ok := e.Data.(core.ToolCallEvent); ok {
					fmt.Fprintf(os.Stdout, "\n[Tool: %s args: %s]\n", ev.Name, ev.Args)
				}
			case core.EventToolDelta:
				if ev, ok := e.Data.(core.ToolCallEvent); ok {
					fmt.Print(ev.Result)
					toolDeltas[ev.ID] += len(ev.Result)
				}
			case core.EventToolResult:
				if ev, ok := e.Data.(core.ToolCallEvent); ok {
					if ev.Error {
						fmt.Fprintf(os.Stdout, "\n[Tool Error: %s]\n", ev.Result)
					} else if toolDeltas[ev.ID] == 0 {
						// Only print result if no delta was streamed
						fmt.Fprintf(os.Stdout, "\n[Tool Result: %s]\n", ev.Result)
					}
				}
			case core.EventLog:
				if entry, ok := e.Data.(core.LogEntry); ok {
					if entry.Level == "err" {
						fmt.Fprintf(os.Stderr, "error: %s\n", entry.Message)
					} else {
						fmt.Fprintf(os.Stderr, "log: %s\n", entry.Message)
					}
				}
			}
		}

		err = ag.Prompt(context.Background(), cfg, query, emit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Execution error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println() // Print a final newline
	} else {
		// Interactive TUI mode
		if err := tui.RunWithPreloads(preloads); err != nil {
			fmt.Fprintf(os.Stderr, "enough: %v\n", err)
			os.Exit(1)
		}
	}
}

func parseSkillsList(s string) []string {
	var list []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			list = append(list, part)
		}
	}
	return list
}

func printUsage() {
	fmt.Println("Usage: enough [options] [query]")
	fmt.Println("\nOptions:")
	fmt.Println("  --skills <skills>     Comma-separated list of skills to preload")
	fmt.Println("  -q, --query <query>   Single query to execute, then exit")
	fmt.Println("  skills <action>       Manage skills (run 'enough skills' for actions)")
}
