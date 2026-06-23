// PORT: backend/agent/obligations/match.go

const curl_cmd_re = /\bcurl(?:\s+-[\w-]+)*\s+https?:\/\/[^\s"'`]+/gi;

export const extract_task_verify_commands = (prompt: string): string[] => {
  const seen = new Set<string>();
  const out: string[] = [];
  const add = (cmd: string): void => {
    cmd = cmd.trim();
    if (cmd === "" || seen.has(cmd)) {
      return;
    }
    seen.add(cmd);
    out.push(cmd);
  };

  for (const m of prompt.match(curl_cmd_re) ?? []) {
    add(m);
  }

  const parts = prompt.split("`");
  for (let i = 1; i < parts.length; i += 2) {
    const part = parts[i].trim();
    if (looks_like_verify_command(part)) {
      add(part);
    }
  }
  return out;
};

export const looks_like_verify_command = (command: string): boolean => {
  const lower = command.trim().toLowerCase();
  if (lower === "") {
    return false;
  }
  if (lower.startsWith("curl ") || lower.includes(" curl ")) {
    return true;
  }
  for (const prefix of [
    "go test",
    "pytest",
    "npm test",
    "pnpm test",
    "yarn test",
    "cargo test",
    "make test",
    "vitest",
    "jest",
  ]) {
    if (lower.startsWith(prefix)) {
      return true;
    }
  }
  return false;
};

export const is_verify_command = (
  command: string,
  verify_cmd: string,
  extra: string[],
): boolean => {
  if (command_matches_any(command, verify_cmd, extra)) {
    return true;
  }
  return looks_like_verify_command(command);
};

export const command_matches_any = (
  command: string,
  verify_cmd: string,
  extra: string[],
): boolean => {
  if (verify_cmd !== "" && command_matches_pattern(command, verify_cmd)) {
    return true;
  }
  for (const pattern of extra) {
    if (command_matches_pattern(command, pattern)) {
      return true;
    }
  }
  return false;
};

export const command_matches_pattern = (command: string, pattern: string): boolean => {
  command = command.trim();
  pattern = pattern.trim();
  if (pattern === "") {
    return false;
  }
  if (command.includes(pattern)) {
    return true;
  }
  const url = extract_curl_url(pattern);
  if (url !== "" && command.includes(url)) {
    return true;
  }
  return false;
};

export const extract_curl_url = (command: string): string => {
  const fields = command.split(/\s+/);
  for (const f of fields) {
    if (f.startsWith("http://") || f.startsWith("https://")) {
      return f.replace(/["']/g, "");
    }
  }
  return "";
};

/*
PORT STATUS
source path: backend/agent/obligations/match.go
source lines: 97
draft lines: 121
confidence: high
status: phase_b_compile
todos:
  - confirm the RegExp flag (?i) matches Go's case-insensitive matching
notes:
  - No (T, error) returns; plain function port.
*/
