// PORT: backend/agent/bash_sanitize.go

const bashBlockedPatterns = [
  { re: /\bmpv\b/i, hint: "mpv draws video/sixel into stdout and breaks the Hollow TUI" },
  { re: /--vo=(sixel|tct|caca|kitty)/i, hint: "terminal video output cannot run inside Hollow" },
  { re: /\bffmpeg\b.*\bpix_fmt=sixel\b/i, hint: "sixel ffmpeg output breaks the TUI" },
  { re: /\bchafa\b/i, hint: "terminal image output breaks the TUI" },
  { re: /\bimg2sixel\b/i, hint: "sixel output breaks the TUI" },
  { re: /\bviu\b/i, hint: "terminal image output breaks the TUI" },
  { re: /\bsxiv\b/i, hint: "terminal image viewer breaks the TUI" },
];

export function bashCommandBlocked(command: string): string {
  const cmd = command.trim();
  if (cmd === "") {
    return "";
  }
  for (const p of bashBlockedPatterns) {
    if (p.re.test(cmd)) {
      return "REJECTED: " + p.hint + ". Run it in an external terminal, not via the bash tool. Use plain-text checks (curl, tests, file inspection) here.";
    }
  }
  return "";
}

// SanitizeBashOutput strips terminal escape sequences (CSI, OSC, DCS/sixel, etc.)
// so bash tool output cannot corrupt the Hollow TUI. Returns cleaned text and
// whether significant binary/escape content was removed.
export function SanitizeBashOutput(inStr: string): [string, boolean] {
  if (inStr === "") {
    return ["", false];
  }
  const chunks: string[] = [];
  const rawLen = inStr.length;
  let i = 0;
  while (i < rawLen) {
    const c = inStr[i];
    const code = inStr.charCodeAt(i);
    if (code === 0x1b) {
      i = skipEscapeSequence(inStr, i);
      continue;
    }
    if (c === "\n" || c === "\r" || c === "\t" || code >= 0x20) {
      chunks.push(c);
    }
    i++;
  }
  let out = chunks.join("");
  out = stripOrphanTerminalLeaks(out);
  const suppressed = rawLen > 32 && out.length < rawLen / 2;
  if (out === "" && rawLen > 0) {
    return [
      "[terminal graphics/control output suppressed — do not run mpv/sixel/TUI apps via bash; use an external terminal]",
      true,
    ];
  }
  if (suppressed) {
    out = out.replace(/\n+$/, "") + "\n[… terminal escape sequences stripped from bash output]";
  }
  return [out, suppressed];
}

function skipEscapeSequence(s: string, i: number): number {
  if (i >= s.length || s.charCodeAt(i) !== 0x1b) {
    return i + 1;
  }
  i++;
  if (i >= s.length) {
    return i;
  }
  const c = s[i];
  switch (c) {
    case "[": // CSI
      i++;
      while (i < s.length && (s.charCodeAt(i) < 0x40 || s.charCodeAt(i) > 0x7e)) {
        i++;
      }
      if (i < s.length) {
        i++;
      }
      break;
    case "]": // OSC
      i++;
      while (i < s.length) {
        if (s.charCodeAt(i) === 0x07) {
          return i + 1;
        }
        if (s.charCodeAt(i) === 0x1b && i + 1 < s.length && s[i + 1] === "\\") {
          return i + 2;
        }
        i++;
      }
      break;
    case "P": // DCS — sixel lives here
      i++;
      while (i < s.length) {
        if (s.charCodeAt(i) === 0x1b && i + 1 < s.length && s[i + 1] === "\\") {
          return i + 2;
        }
        if (s.charCodeAt(i) === 0x9c) {
          return i + 1;
        }
        i++;
      }
      break;
    case "(":
    case ")":
    case "*":
    case "+":
    case "-":
    case ".":
    case "/": // two-char
      return i + 2;
    default:
      return i + 1;
  }
  return i;
}

// stripOrphanTerminalLeaks removes CSI/mouse bytes left behind when ESC (0x1b)
// was consumed by the terminal emulator before capture — shows up as [MCX0…
function stripOrphanTerminalLeaks(inStr: string): string {
  if (inStr === "") {
    return inStr;
  }
  // Mouse reports without leading ESC: \033[M + 3 bytes → [M + 3 bytes
  const countM = (inStr.match(/\[M/g) || []).length;
  if (countM >= 4) {
    const chunks: string[] = [];
    let i = 0;
    while (i < inStr.length) {
      if (i + 2 <= inStr.length && inStr[i] === "[" && inStr[i + 1] === "M") {
        i += 2;
        for (let j = 0; j < 3 && i < inStr.length; j++) {
          i++;
        }
        continue;
      }
      // Orphan CSI: [ params final-byte (@ through ~)
      if (inStr[i] === "[") {
        let j = i + 1;
        while (j < inStr.length && inStr.charCodeAt(j) >= 0x20 && inStr.charCodeAt(j) <= 0x3f) {
          j++;
        }
        if (j < inStr.length && inStr.charCodeAt(j) >= 0x40 && inStr.charCodeAt(j) <= 0x7e) {
          i = j + 1;
          continue;
        }
      }
      chunks.push(inStr[i]);
      i++;
    }
    const clean = chunks.join("");
    if (countM >= 4 && clean.trim().length < inStr.length / 4) {
      return "[terminal mouse/control output suppressed — do not run mpv/sixel/TUI apps via bash; use an external terminal]";
    }
    return clean;
  }
  return inStr;
}

/*
PORT STATUS
source path: backend/agent/bash_sanitize.go
source lines: 156
draft lines: 147
confidence: high
status: phase_b_compile
*/
