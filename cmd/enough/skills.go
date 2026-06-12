package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/skills"
)

func runSkillsCLI() {
	if len(os.Args) < 3 {
		printSkillsUsage()
		os.Exit(1)
	}

	action := strings.ToLower(os.Args[2])
	args := os.Args[3:]

	switch action {
	case "browse":
		browseSkills(args)
	case "search":
		searchSkills(args)
	case "install":
		installSkill(args)
	case "inspect":
		inspectSkill(args)
	case "list":
		listSkills(args)
	case "check":
		checkSkills(args)
	case "update":
		updateSkills(args)
	case "audit":
		auditSkills(args)
	case "uninstall":
		uninstallSkill(args)
	case "reset":
		resetSkill(args)
	case "sync":
		syncSkills(args)
	case "configure":
		configureSkills(args)
	default:
		fmt.Printf("Unknown skills action: %s\n", action)
		printSkillsUsage()
		os.Exit(1)
	}
}

func printSkillsUsage() {
	fmt.Println("Usage: enough skills <action> [options]")
	fmt.Println("\nActions:")
	fmt.Println("  browse [--page N] [--size N] [--source SOURCE]")
	fmt.Println("  search <query> [--source SOURCE] [--limit N] [--json]")
	fmt.Println("  install <identifier> [--category CAT] [--name NAME] [--force] [-y]")
	fmt.Println("  inspect <identifier>")
	fmt.Println("  list [--source all|hub|builtin|local] [--enabled-only]")
	fmt.Println("  check [name]")
	fmt.Println("  update [name]")
	fmt.Println("  audit [name] [--deep]")
	fmt.Println("  uninstall <name>")
	fmt.Println("  reset <name> [--restore] [-y]")
	fmt.Println("  sync")
	fmt.Println("  configure")
}

func browseSkills(args []string) {
	fs := flag.NewFlagSet("browse", flag.ExitOnError)
	page := fs.Int("page", 1, "")
	size := fs.Int("size", 20, "")
	source := fs.String("source", "all", "")
	_ = fs.Parse(args)

	sources := skills.CreateSourceRouter(nil)
	results := skills.UnifiedSearch("", sources, *source, 1000)

	total := len(results)
	if total == 0 {
		fmt.Println("No skills found in the Skills Hub.")
		return
	}

	pageSize := *size
	if pageSize < 1 {
		pageSize = 20
	}
	totalPages := (total + pageSize - 1) / pageSize
	currentPage := *page
	if currentPage < 1 {
		currentPage = 1
	}
	if currentPage > totalPages {
		currentPage = totalPages
	}

	start := (currentPage - 1) * pageSize
	end := start + pageSize
	if end > total {
		end = total
	}

	pageItems := results[start:end]

	fmt.Printf("\nSkills Hub — Browse — %s  (%d skills loaded, page %d/%d)\n\n", *source, total, currentPage, totalPages)

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "#\tName\tDescription\tSource\tTrust\tIdentifier")
	for i, r := range pageItems {
		idx := start + i + 1
		desc := r.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", idx, r.Name, desc, r.Source, r.TrustLevel, r.Identifier)
	}
	w.Flush()

	fmt.Println()
	if currentPage > 1 {
		fmt.Printf("  --page %d ← prev | ", currentPage-1)
	}
	if currentPage < totalPages {
		fmt.Printf("  --page %d → next", currentPage+1)
	}
	fmt.Println()
}

func searchSkills(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: enough skills search <query> [--source SOURCE] [--limit N] [--json]")
		os.Exit(1)
	}
	query := args[0]

	fs := flag.NewFlagSet("search", flag.ExitOnError)
	source := fs.String("source", "all", "")
	limit := fs.Int("limit", 10, "")
	asJSON := fs.Bool("json", false, "")
	_ = fs.Parse(args[1:])

	sources := skills.CreateSourceRouter(nil)
	results := skills.UnifiedSearch(query, sources, *source, *limit)

	if *asJSON {
		data, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(results) == 0 {
		fmt.Println("No skills found matching your query.")
		return
	}

	fmt.Printf("\nSearching for: %s\n\n", query)

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "Name\tDescription\tSource\tTrust\tIdentifier")
	for _, r := range results {
		desc := r.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, desc, r.Source, r.TrustLevel, r.Identifier)
	}
	w.Flush()
	fmt.Println()
}

func installSkill(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: enough skills install <identifier> [--category CAT] [--name NAME] [--force] [-y]")
		os.Exit(1)
	}
	identifier := args[0]

	fs := flag.NewFlagSet("install", flag.ExitOnError)
	category := fs.String("category", "", "")
	name := fs.String("name", "", "")
	force := fs.Bool("force", false, "")
	yesPtr := fs.Bool("yes", false, "")
	yPtr := fs.Bool("y", false, "")
	_ = fs.Parse(args[1:])

	yes := *yesPtr || *yPtr

	fmt.Printf("Fetching: %s...\n", identifier)
	dir, err := skills.InstallSkill(identifier, *category, *name, *force, yes, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nSuccessfully installed skill to: %s\n", dir)
}

func inspectSkill(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: enough skills inspect <identifier>")
		os.Exit(1)
	}
	identifier := args[0]

	meta, bundle, err := skills.InspectSkill(identifier, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Name:        %s\n", meta.Name)
	fmt.Printf("Description: %s\n", meta.Description)
	fmt.Printf("Source:      %s\n", meta.Source)
	fmt.Printf("Trust:       %s\n", meta.TrustLevel)
	fmt.Printf("Identifier:  %s\n", meta.Identifier)
	if len(meta.Tags) > 0 {
		fmt.Printf("Tags:        %s\n", strings.Join(meta.Tags, ", "))
	}

	if bundle != nil {
		if content, ok := bundle.Files["SKILL.md"]; ok {
			lines := strings.Split(string(content), "\n")
			fmt.Println("\nSKILL.md Preview:")
			limit := 50
			if len(lines) < limit {
				limit = len(lines)
			}
			for i := 0; i < limit; i++ {
				fmt.Println(lines[i])
			}
			if len(lines) > 50 {
				fmt.Printf("\n... (%d more lines)\n", len(lines)-50)
			}
		}
	}
	fmt.Println()
}

func listSkills(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	source := fs.String("source", "all", "")
	enabledOnly := fs.Bool("enabled-only", false, "")
	_ = fs.Parse(args)

	cfg, err := config.LoadRuntime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	allSkills, _ := skills.DiscoverAllSkills(cwd, cfg)

	lock := skills.LoadLockFile()
	hubInstalled := make(map[string]skills.InstalledSkill)
	for _, entry := range lock.ListInstalled() {
		name := ""
		if n, ok := entry.Metadata["name"].(string); ok {
			name = n
		} else {
			name = filepath.Base(entry.InstallPath)
		}
		hubInstalled[name] = entry
	}
	builtinNames := skills.ReadManifest()

	title := "Installed Skills"
	if *enabledOnly {
		title += " (enabled only)"
	}
	fmt.Printf("\n%s:\n\n", title)

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "Name\tCategory\tSource\tTrust\tStatus")

	hubCount, builtinCount, localCount := 0, 0, 0
	enabledCount, disabledCount := 0, 0

	sort.Slice(allSkills, func(i, j int) bool {
		if allSkills[i].Category != allSkills[j].Category {
			return allSkills[i].Category < allSkills[j].Category
		}
		return allSkills[i].Name < allSkills[j].Name
	})

	for _, sk := range allSkills {
		sourceType := "local"
		sourceDisplay := "local"
		trust := "local"

		if hubEntry, ok := hubInstalled[sk.Name]; ok {
			sourceType = "hub"
			sourceDisplay = hubEntry.Source
			trust = hubEntry.TrustLevel
		} else if _, ok := builtinNames[sk.Name]; ok {
			sourceType = "builtin"
			sourceDisplay = "builtin"
			trust = "builtin"
		}

		if *source != "all" && *source != sourceType {
			continue
		}

		isEnabled := !skills.IsSkillDisabled(sk.Name, cfg)
		if *enabledOnly && !isEnabled {
			continue
		}

		if sourceType == "hub" {
			hubCount++
		} else if sourceType == "builtin" {
			builtinCount++
		} else {
			localCount++
		}

		statusStr := "enabled"
		if isEnabled {
			enabledCount++
		} else {
			disabledCount++
			statusStr = "disabled"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", sk.Name, sk.Category, sourceDisplay, trust, statusStr)
	}
	w.Flush()

	summary := fmt.Sprintf("\n%d hub-installed, %d builtin, %d local", hubCount, builtinCount, localCount)
	if *enabledOnly {
		summary += fmt.Sprintf(" — %d enabled shown", enabledCount)
	} else {
		summary += fmt.Sprintf(" — %d enabled, %d disabled", enabledCount, disabledCount)
	}
	fmt.Println(summary)
}

func checkSkills(args []string) {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	sources := skills.CreateSourceRouter(nil)
	results := skills.CheckForSkillUpdates(name, sources)

	if len(results) == 0 {
		fmt.Println("No matching hub-installed skills to check.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "Name\tIdentifier\tSource\tStatus")
	for _, r := range results {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r["name"], r["identifier"], r["source"], r["status"])
	}
	w.Flush()
}

func updateSkills(args []string) {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	updated, err := skills.UpdateSkills(name, false, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(updated) == 0 {
		fmt.Println("All skills are up to date.")
		return
	}

	fmt.Println("Updated skills:")
	for _, n := range updated {
		fmt.Printf("  - %s\n", n)
	}
}

func auditSkills(args []string) {
	fs := flag.NewFlagSet("audit", flag.ExitOnError)
	deep := fs.Bool("deep", false, "")
	_ = fs.Parse(args)

	name := ""
	if fs.NArg() > 0 {
		name = fs.Arg(0)
	}

	lock := skills.LoadLockFile()
	installed := lock.ListInstalled()

	if len(installed) == 0 {
		fmt.Println("No hub-installed skills to audit.")
		return
	}

	targets := installed
	if name != "" {
		targets = nil
		for _, entry := range installed {
			n := filepath.Base(entry.InstallPath)
			if entry.Metadata != nil {
				if metaName, ok := entry.Metadata["name"].(string); ok {
					n = metaName
				}
			}
			if n == name {
				targets = append(targets, entry)
			}
		}
		if len(targets) == 0 {
			fmt.Printf("Error: '%s' is not a hub-installed skill.\n", name)
			os.Exit(1)
		}
	}

	fmt.Printf("\nAuditing %d skill(s)...\n\n", len(targets))

	skillsDir := skills.SkillsDir()
	for _, entry := range targets {
		n := filepath.Base(entry.InstallPath)
		if entry.Metadata != nil {
			if metaName, ok := entry.Metadata["name"].(string); ok {
				n = metaName
			}
		}
		skillPath := filepath.Join(skillsDir, entry.InstallPath)
		if _, err := os.Stat(skillPath); os.IsNotExist(err) {
			fmt.Printf("Warning: %s — path missing: %s\n", n, entry.InstallPath)
			continue
		}

		scanSource := entry.Identifier
		if scanSource == "" {
			scanSource = entry.Source
		}
		result := skills.ScanSkill(skillPath, scanSource)
		fmt.Println(skills.FormatScanReport(result))

		if *deep {
			findings, err := skills.ASTScanPath(skillPath)
			if err == nil {
				fmt.Println(skills.FormatASTReport(findings, n))
			} else {
				fmt.Printf("AST scan failed: %v\n", err)
			}
		}
		fmt.Println()
	}
}

func uninstallSkill(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: enough skills uninstall <name>")
		os.Exit(1)
	}
	name := args[0]

	ok, msg := skills.UninstallSkill(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		os.Exit(1)
	}
	fmt.Println(msg)
}

func resetSkill(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: enough skills reset <name> [--restore] [-y]")
		os.Exit(1)
	}
	name := args[0]

	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	restore := fs.Bool("restore", false, "")
	yesPtr := fs.Bool("yes", false, "")
	yPtr := fs.Bool("y", false, "")
	_ = fs.Parse(args[1:])

	yes := *yesPtr || *yPtr

	if *restore && !yes {
		fmt.Printf("Delete and restore skill %q from bundled source? [y/N]: ", name)
		var answer string
		_, _ = fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Reset cancelled.")
			return
		}
	}

	ok, msg, _, err := skills.ResetBundledSkill(name, *restore)
	if err != nil || !ok {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
		os.Exit(1)
	}
	fmt.Println(msg)
}

func syncSkills(args []string) {
	fmt.Println("Syncing bundled skills...")
	res, err := skills.SyncSkills(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nSync completed:\n")
	fmt.Printf("  %d skills copied\n", len(res.Copied))
	fmt.Printf("  %d skills updated\n", len(res.Updated))
	fmt.Printf("  %d user-modified skipped\n", len(res.UserModified))
	fmt.Printf("  %d cleaned from manifest\n", len(res.Cleaned))
	fmt.Printf("  %d suppressed\n", len(res.Suppressed))
	fmt.Printf("  %d total tracked\n", res.TotalBundled)
}

func configureSkills(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("\nConfigure skills for:")
	fmt.Println("  1. All platforms (global default)")
	fmt.Println("  2. TUI (cli)")
	fmt.Print("Select [1]: ")

	platform := ""
	if scanner.Scan() {
		sel := strings.TrimSpace(scanner.Text())
		if sel == "2" {
			platform = "cli"
		}
	}

	platformLabel := "All platforms"
	if platform != "" {
		platformLabel = "TUI (cli)"
	}

	fmt.Printf("\nConfigure for: %s\n", platformLabel)
	fmt.Println("  1. Toggle individual skills")
	fmt.Println("  2. Toggle by category")
	fmt.Print("Select [1]: ")

	mode := "1"
	if scanner.Scan() {
		sel := strings.TrimSpace(scanner.Text())
		if sel == "2" {
			mode = "2"
		}
	}

	cwd, _ := os.Getwd()
	runCfg, _ := config.LoadRuntime()
	allSkills, _ := skills.DiscoverAllSkills(cwd, runCfg)

	if len(allSkills) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	disabledSet := make(map[string]bool)
	if platform == "" {
		for _, d := range cfg.Skills.Disabled {
			disabledSet[d] = true
		}
	} else {
		if list, ok := cfg.Skills.PlatformDisabled[platform]; ok {
			for _, d := range list {
				disabledSet[d] = true
			}
		}
	}

	if mode == "1" {
		// Individual toggle loop
		for {
			fmt.Printf("\n--- Skills list (%s) ---\n", platformLabel)
			for i, sk := range allSkills {
				status := "✓"
				if disabledSet[sk.Name] {
					status = "✗"
				}
				fmt.Printf("  %d. [%s] %s  (%s)\n", i+1, status, sk.Name, sk.Category)
			}
			fmt.Print("\nEnter number to toggle, or 'q' to save and exit: ")
			if !scanner.Scan() {
				break
			}
			text := strings.TrimSpace(scanner.Text())
			if strings.ToLower(text) == "q" || text == "" {
				break
			}
			idx, err := strconv.Atoi(text)
			if err == nil && idx >= 1 && idx <= len(allSkills) {
				skName := allSkills[idx-1].Name
				disabledSet[skName] = !disabledSet[skName]
				fmt.Printf("Toggled %s\n", skName)
			} else {
				fmt.Println("Invalid input.")
			}
		}
	} else {
		// Category toggle loop
		catMap := make(map[string][]string)
		for _, sk := range allSkills {
			cat := sk.Category
			if cat == "" {
				cat = "uncategorized"
			}
			catMap[cat] = append(catMap[cat], sk.Name)
		}

		var categories []string
		for cat := range catMap {
			categories = append(categories, cat)
		}
		sort.Strings(categories)

		for {
			fmt.Printf("\n--- Categories list (%s) ---\n", platformLabel)
			for i, cat := range categories {
				// Category is enabled if at least one skill inside is not disabled
				allDisabled := true
				for _, skName := range catMap[cat] {
					if !disabledSet[skName] {
						allDisabled = false
						break
					}
				}
				status := "✓"
				if allDisabled {
					status = "✗"
				}
				fmt.Printf("  %d. [%s] %s (%d skills)\n", i+1, status, cat, len(catMap[cat]))
			}
			fmt.Print("\nEnter number to toggle, or 'q' to save and exit: ")
			if !scanner.Scan() {
				break
			}
			text := strings.TrimSpace(scanner.Text())
			if strings.ToLower(text) == "q" || text == "" {
				break
			}
			idx, err := strconv.Atoi(text)
			if err == nil && idx >= 1 && idx <= len(categories) {
				catName := categories[idx-1]
				// Toggle category status: if currently not all disabled, disable all. Otherwise enable all.
				anyEnabled := false
				for _, skName := range catMap[catName] {
					if !disabledSet[skName] {
						anyEnabled = true
						break
					}
				}
				for _, skName := range catMap[catName] {
					disabledSet[skName] = anyEnabled
				}
				fmt.Printf("Toggled category %s\n", catName)
			} else {
				fmt.Println("Invalid input.")
			}
		}
	}

	var newDisabled []string
	for skName, isDisabled := range disabledSet {
		if isDisabled {
			newDisabled = append(newDisabled, skName)
		}
	}
	sort.Strings(newDisabled)

	if platform == "" {
		cfg.Skills.Disabled = newDisabled
	} else {
		if cfg.Skills.PlatformDisabled == nil {
			cfg.Skills.PlatformDisabled = make(map[string][]string)
		}
		cfg.Skills.PlatformDisabled[platform] = newDisabled
	}

	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nSaved configuration: %d skills disabled under %s.\n", len(newDisabled), platformLabel)
}
