# Handoff: Port Flame keyboard input + Ctrl+C from pi-tui

Give this entire doc to the implementing agent. **Do not add one-off Kitty parsers for individual keys.** Port the Flame layer properly.

## Goal

Enough's composer Ctrl+C is broken on modern terminals (Warp, Kitty, modifyOtherKeys). Partial fixes (`parseKittyKeyU` only for `99;5u`, etc.) are **not acceptable**. Port Flame's (`@mariozechner/pi-tui` / `flame-tui`) **stdin buffering + `matchesKey` + app.clear semantics** into Enough's Go TUI.

## Flame source of truth (local checkout)

```
/home/idk/projects/flame/packages/tui/src/keys.ts          # matchesKey, parseKey, Kitty/modifyOtherKeys
/home/idk/projects/flame/packages/tui/src/stdin-buffer.ts # complete escape sequence reassembly
/home/idk/projects/flame/packages/tui/test/keys.test.ts   # golden tests — port or transcribe
/home/idk/projects/flame/packages/tui/src/components/editor.ts  # ctrl+c delegated to parent via tui.input.copy
/home/idk/projects/flame/packages/coding-agent/src/modes/interactive/interactive-mode.ts
  - handleCtrlC() ~3268
  - clearEditor() ~3611
  - app.clear registered ~2424
/home/idk/projects/flame/packages/coding-agent/src/core/keybindings.ts
  - app.clear = ctrl+c, app.exit = ctrl+d
```

## Enough files to replace/extend (not patch ad hoc)

```
frontend/tui/keys.go           # keyReader — today a slim parseOne; needs matchesKey-style API
frontend/tui/stdin_buffer.go   # partial Flame port; missing timeout flush, Kitty dedupe, DCS/OSC completeness
frontend/tui/interrupt.go      # handleCtrlC / handleCtrlD — wrong semantics vs Flame
frontend/tui/app.go            # handleKey routing, dispatchKeyInput render
frontend/tui/term/drain.go     # already exists; Flame drains on shutdown too
```

## What Flame actually does (copy this behavior)

### 1. Key detection — `matchesKey(data, keyId)`

One function handles **all** encodings for a logical key:

| Encoding | Ctrl+C example |
|----------|----------------|
| Legacy raw | `\x03` |
| Kitty CSI-u | `\x1b[99;5u` |
| Kitty + base layout (Cyrillic) | `\x1b[1089::99;5u` |
| xterm modifyOtherKeys | `\x1b[27;5;99~` |
| Key release (must NOT fire action) | `\x1b[99;5:3u` — ignore for app.clear |

See `keys.test.ts` cases: `"should match legacy Ctrl+c"`, `"should match xterm modifyOtherKeys Ctrl+c"`, `"should match Ctrl+c when pressing Ctrl+С (Cyrillic)"`, release events.

**Enough today:** `parseOne()` hardcodes a few Kitty sequences then **discards unknown CSI** (`return parsedKey{}, n`). That's why Warp Ctrl+C vanishes.

**Required:** Port `matchesKey` / `parseKey` (or call into a Go translation of `keys.ts`). Route **complete sequences** from stdin buffer → `matchesKey(seq, "ctrl+c")` → action, not byte-at-a-time special cases.

### 2. Stdin buffer — `StdinBuffer`

Flame accumulates partial escapes, handles bracketed paste, dedupes Kitty printable+CSI double-send, 10ms timeout flush for lone ESC.

**Enough today:** `stdin_buffer.go` is a simplified subset. Port remaining behavior from `stdin-buffer.ts` or document intentional omissions with tests.

### 3. Editor does NOT swallow Ctrl+C

Flame `editor.ts` ~584:

```ts
if (kb.matches(data, "tui.input.copy")) {
  return; // parent handles app.clear
}
```

Enough must **not** insert ctrl+c text or discard the sequence before `handleCtrlC`.

### 4. `app.clear` / Ctrl+C semantics (Flame interactive-mode)

```ts
private handleCtrlC(): void {
  const now = Date.now();
  if (now - this.lastSigintTime < 500) {
    void this.shutdown();      // second press within 500ms → exit
  } else {
    this.clearEditor();        // first press → clear composer (even if empty)
    this.lastSigintTime = now;
  }
}
```

`clearEditor()` = `editor.setText("")` + `requestRender()`.

**UI hint (Flame):** `"ctrl+c twice to exit"` when idle.

**Enough today (wrong):** custom logic (clear if draft else quit immediately; or abort agent when running). **Replace with Flame semantics** unless product owner explicitly overrides after port.

### 5. `app.exit` / Ctrl+D

Flame: `handleCtrlD()` → shutdown **only when editor empty** (editor blocks Ctrl+D when text present).

Enough: align with Flame after `matchesKey` works for `\x04` and `\x1b[100;5u`.

### 6. Shutdown drain (Flame)

```ts
await this.ui.terminal.drainInput(1000); // before exit — prevents escape leak to shell
```

Enough has `term.DrainInput` — call from quit path like Flame `shutdown()`.

## Implementation plan (ordered)

1. **Port `matchesKey` + `parseKey` to Go** (`frontend/tui/keys_match.go` or replace `keys.go`).
   - Start by transcribing `keys.test.ts` → Go table tests; make them pass.
   - Do NOT ship until `\x03`, `\x1b[99;5u`, `\x1b[27;5;99~`, Cyrillic base-layout, and release-event cases match Flame.

2. **Upgrade `stdin_buffer.go`** to emit **one complete sequence string** per Flame `StdinBuffer.process()` event.

3. **Change input pipeline:**
   ```
   term readLoop → stdinBuffer.process → for each sequence:
     if matchesKey(seq, "ctrl+c") → handleCtrlC()
     else if matchesKey(seq, "ctrl+d") → handleCtrlD()
     else → existing editor key handling (or parseKey for editor bindings)
   ```

4. **Replace `handleCtrlC` / `handleCtrlD`** with Flame logic + restore `lastSigintTime`.

5. **Wire shutdown drain** in `app.run()` quit path / `term.Stop()`.

6. **Delete** ad-hoc `parseKittyKeyU` special cases once `matchesKey` covers them.

## Tests required before merge

- Go tests transcribing Flame `packages/tui/test/keys.test.ts` ctrl+c / ctrl+d / modifyOtherKeys / Cyrillic / release
- `TestHandleCtrlC` — first press clears, second within 500ms quits (Flame behavior)
- Manual: Warp + Alacritty — type text, Ctrl+C clears; double Ctrl+C exits; no garbage on shell prompt

## Explicit anti-patterns (do NOT do these)

- ❌ Adding another `parseKittyKeyU(b, XX, 5, keyCtrlX)` one-liner per key
- ❌ Discarding unknown CSI in `parseOne` without matching Flame's full parser
- ❌ SIGINT/process signal handler for composer clear (raw mode eats SIGINT)
- ❌ Different clear/quit semantics without documenting deviation from Flame

## Repo context

- Enough: `/home/idk/projects/Enough`
- Flame: `/home/idk/projects/flame`
- Build: `make build` → `bin/enough`
- Recent terminal leak fix: `frontend/tui/term/drain.go`, `markdown/terminal_probe.go` (separate from this task)

## Success criteria

User on **Warp** and **Alacritty** can:
1. Type in composer → **Ctrl+C clears text** (first press)
2. **Ctrl+C twice within 500ms** exits Enough (Flame behavior)
3. **Ctrl+D** exits when composer empty
4. No escape garbage on shell after exit
