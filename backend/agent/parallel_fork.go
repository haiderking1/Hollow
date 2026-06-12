package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const parallelForkMaxTurns = 20

var forkAngles = []string{
	"Approach A: minimal diff — fix the root cause only.",
	"Approach B: re-read the failing code path and patch the smallest broken unit.",
	"Approach C: add a quick diagnostic, then fix with evidence from its output.",
	"Approach D: try an alternative implementation that still satisfies the goal.",
}

type forkOutcome struct {
	index    int
	workDir  string
	exitCode int
	output   string
	worker   swarmWorkerResult
}

func (a *Agent) runParallelForks(ctx context.Context, lockedGoal, lastFailure, verifyCmd string, count int) (summary string, merged bool) {
	repoRoot, err := repoRootOf(a.workDir)
	if err != nil {
		return "No git repo — parallel forks skipped (use agent_swarm with isolate=worktree for manual parallel attempts).", false
	}

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	type slot struct {
		dir    string
		branch string
	}
	slots := make([]slot, 0, count)
	defer func() {
		for _, s := range slots {
			_ = git(repoRoot, "worktree", "remove", "--force", s.dir)
			_ = git(repoRoot, "branch", "-D", s.branch)
		}
	}()

	for i := 0; i < count; i++ {
		if userAbortFired(ctx) {
			return "Parallel forks aborted.", false
		}
		id := fmt.Sprintf("fork-%d", i+1)
		branch := fmt.Sprintf("enough-fork/%s/%s", runID, id)
		base := filepath.Join(os.TempDir(), "enough-fork-"+runID)
		dir := filepath.Join(base, id)
		if err := os.MkdirAll(base, 0o755); err != nil {
			continue
		}
		if err := git(repoRoot, "worktree", "add", "-b", branch, dir, "HEAD"); err != nil {
			continue
		}
		slots = append(slots, slot{dir: dir, branch: branch})
	}

	if len(slots) == 0 {
		return "Could not create git worktrees for parallel forks.", false
	}

	tasks := make([]swarmTask, len(slots))
	for i := range slots {
		angle := forkAngles[i%len(forkAngles)]
		tasks[i] = swarmTask{
			ID:     fmt.Sprintf("fork-%d", i+1),
			Prompt: buildForkPrompt(lockedGoal, lastFailure, verifyCmd, angle),
		}
	}

	var outcomes []forkOutcome
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, task swarmTask, dir string) {
			defer wg.Done()
			if userAbortFired(ctx) {
				return
			}

			worker := &Agent{
				cfg:        a.cfg,
				client:     a.client,
				workDir:    dir,
				swarmDepth: 1,
			}
			result := worker.runSwarmWorkerInDir(ctx, task, idx, 1, "", 0, parallelForkMaxTurns, dir)
			exitCode, verifyOut := runBashInDir(dir, verifyCmd)

			mu.Lock()
			outcomes = append(outcomes, forkOutcome{
				index:    idx,
				workDir:  dir,
				exitCode: exitCode,
				output:   verifyOut,
				worker:   result,
			})
			mu.Unlock()
		}(i, task, slots[i].dir)
	}
	wg.Wait()

	var winner *forkOutcome
	for i := range outcomes {
		if outcomes[i].exitCode == 0 {
			winner = &outcomes[i]
			break
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Workers: %d.", len(outcomes))
	for _, o := range outcomes {
		status := "fail"
		if o.exitCode == 0 {
			status = "pass"
		}
		fmt.Fprintf(&b, "\n- fork-%d: verify=%s worker=%s", o.index+1, status, o.worker.Status)
	}

	if winner == nil {
		return b.String(), false
	}

	if err := syncWorktreeChanges(winner.workDir, a.workDir); err != nil {
		b.WriteString("\nWinner found but merge failed: " + err.Error())
		return b.String(), false
	}

	b.WriteString(fmt.Sprintf("\nApplied patch from fork-%d.", winner.index+1))
	a.noteMutation()
	return b.String(), true
}

func buildForkPrompt(lockedGoal, lastFailure, verifyCmd, angle string) string {
	var b strings.Builder
	b.WriteString("GOAL LOCK — complete exactly this task:\n")
	b.WriteString(lockedGoal)
	b.WriteString("\n\nVerification has failed repeatedly. ")
	if lastFailure != "" {
		b.WriteString("Latest failure output:\n")
		b.WriteString(lastFailure)
		b.WriteString("\n\n")
	}
	if verifyCmd != "" {
		b.WriteString("Verification command: ")
		b.WriteString(verifyCmd)
		b.WriteString("\n\n")
	}
	b.WriteString(angle)
	b.WriteString("\nRun verification before finishing.")
	return b.String()
}

func runBashInDir(workDir, command string) (int, string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return -1, "no verify command"
	}
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		return 0, text
	}
	code := -1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	}
	return code, text
}

func syncWorktreeChanges(srcDir, dstDir string) error {
	status, err := gitOutput(srcDir, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("winning worktree has no changes")
	}
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		if err := copyFile(filepath.Join(srcDir, path), filepath.Join(dstDir, path)); err != nil {
			return fmt.Errorf("copy %s: %w", path, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
