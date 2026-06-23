// PORT: backend/agent/edit_file.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";

export function editFileTool(): tool {
  const schema = {
    type: "object",
    properties: {
      path: { type: "string", description: "File path" },
      old_string: { type: "string", description: "Exact text to find, including whitespace" },
      new_string: { type: "string", description: "Replacement text" },
      replace_all: { type: "boolean", description: "Replace every occurrence instead of requiring a unique match" }
    },
    required: ["path", "old_string", "new_string"]
  };
  return {
    type: "function",
    function: {
      name: "edit_file",
      description: "Replace exact text in an existing file. old_string must match uniquely unless replace_all is true.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

Agent.prototype.toolEditFile = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: {
      path: string;
      old_string: string;
      new_string: string;
      replace_all?: boolean;
    };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    const resolvedPath = yield* this.resolvePath(args.path);

    const data = yield* Effect.tryPromise({
      try: async () => await fs.readFile(resolvedPath, "utf8"),
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });

    const [newContent, count, editErr] = applyEdit(data, args.old_string, args.new_string, !!args.replace_all);
    if (editErr !== null) {
      return { output: editErr.message, isErr: true };
    }

    yield* Effect.tryPromise({
      try: async () => await fs.writeFile(resolvedPath, newContent, "utf8"),
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });

    const diff = computeDiff(data, newContent);

    return {
      output: `edited ${resolvedPath} (${count} replacement(s))`,
      details: JSON.stringify({ diff }),
    };
  }).pipe(
    Effect.catchAll((err) =>
      Effect.succeed({ output: err.message, isErr: true })
    )
  );
};

export function replaceMatch(
  content: string,
  oldStr: string,
  newStr: string,
  replaceAll: boolean
): [string, number] {
  const indices: number[] = [];
  let pos = content.indexOf(oldStr);
  while (pos !== -1) {
    indices.push(pos);
    pos = content.indexOf(oldStr, pos + oldStr.length);
  }

  if (indices.length === 0) {
    return [content, 0];
  }

  const matchesToReplace = replaceAll ? indices : [indices[0]];
  let result = content;

  for (let idx = matchesToReplace.length - 1; idx >= 0; idx--) {
    const startIdx = matchesToReplace[idx];
    const endIdx = startIdx + oldStr.length;

    let actualStart = startIdx;
    let actualEnd = endIdx;

    if (newStr === "") {
      const isStartOfLine = startIdx === 0 || result[startIdx - 1] === "\n" || result[startIdx - 1] === "\r";
      const isEndOfLine = endIdx === result.length || result[endIdx] === "\n" || result[endIdx] === "\r";

      if (isStartOfLine && isEndOfLine) {
        if (endIdx < result.length) {
          if (result[endIdx] === "\r" && result[endIdx + 1] === "\n") {
            actualEnd = endIdx + 2;
          } else if (result[endIdx] === "\n") {
            actualEnd = endIdx + 1;
          } else if (result[endIdx] === "\r") {
            actualEnd = endIdx + 1;
          }
        } else if (startIdx > 0) {
          if (result[startIdx - 2] === "\r" && result[startIdx - 1] === "\n") {
            actualStart = startIdx - 2;
          } else if (result[startIdx - 1] === "\n") {
            actualStart = startIdx - 1;
          } else if (result[startIdx - 1] === "\r") {
            actualStart = startIdx - 1;
          }
        }
      }
    }

    result = result.slice(0, actualStart) + newStr + result.slice(actualEnd);
  }

  return [result, matchesToReplace.length];
}

export function applyEdit(
  content: string,
  oldStr: string,
  newStr: string,
  replaceAll: boolean
): [string, number, Error | null] {
  if (oldStr === "") {
    return ["", 0, new Error("old_string is required")];
  }

  let count = 0;
  let pos = content.indexOf(oldStr);
  while (pos !== -1) {
    count++;
    pos = content.indexOf(oldStr, pos + oldStr.length);
  }

  if (count === 0) {
    return ["", 0, new Error("old_string not found in file")];
  }

  if (!replaceAll) {
    if (count > 1) {
      return ["", 0, new Error(`old_string is not unique (${count} matches); add context or set replace_all`)];
    }
  }

  const [newContent, replacedCount] = replaceMatch(content, oldStr, newStr, replaceAll);
  return [newContent, replacedCount, null];
}

export function computeDiff(originalContent: string, newContent: string): string {
  const originalLines = originalContent.split("\n");
  const newLines = newContent.split("\n");

  let start = 0;
  while (start < originalLines.length && start < newLines.length && originalLines[start] === newLines[start]) {
    start++;
  }

  let endOrig = originalLines.length - 1;
  let endNew = newLines.length - 1;
  while (endOrig >= start && endNew >= start && originalLines[endOrig] === newLines[endNew]) {
    endOrig--;
    endNew--;
  }

  // Include up to 3 lines of context before
  const contextBeforeStart = Math.max(0, start - 3);
  // Include up to 3 lines of context after
  const contextAfterEndOrig = Math.min(originalLines.length - 1, endOrig + 3);

  const diffLines: string[] = [];

  // 1. Context before the change
  for (let l = contextBeforeStart; l < start; l++) {
    const lineNum = String(l + 1).padStart(4, " ");
    diffLines.push(` ${lineNum} ${originalLines[l]}`);
  }

  // 2. The change itself (LCS on originalLines[start..endOrig] vs newLines[start..endNew])
  const subOrig = originalLines.slice(start, endOrig + 1);
  const subNew = newLines.slice(start, endNew + 1);

  const dp: number[][] = Array.from({ length: subOrig.length + 1 }, () =>
    new Array(subNew.length + 1).fill(0)
  );

  for (let i = 1; i <= subOrig.length; i++) {
    for (let j = 1; j <= subNew.length; j++) {
      if (subOrig[i - 1] === subNew[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  const subDiff: string[] = [];
  let i = subOrig.length;
  let j = subNew.length;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && subOrig[i - 1] === subNew[j - 1]) {
      const origLineIdx = start + i - 1;
      const lineNum = String(origLineIdx + 1).padStart(4, " ");
      subDiff.push(` ${lineNum} ${subOrig[i - 1]}`);
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      const newLineIdx = start + j - 1;
      const lineNum = String(newLineIdx + 1).padStart(4, " ");
      subDiff.push(`+${lineNum} ${subNew[j - 1]}`);
      j--;
    } else {
      const origLineIdx = start + i - 1;
      const lineNum = String(origLineIdx + 1).padStart(4, " ");
      subDiff.push(`-${lineNum} ${subOrig[i - 1]}`);
      i--;
    }
  }
  diffLines.push(...subDiff.reverse());

  // 3. Context after the change
  const contextAfterCount = Math.min(
    originalLines.length - 1 - endOrig,
    newLines.length - 1 - endNew,
    3
  );
  for (let l = 1; l <= contextAfterCount; l++) {
    const origLineIdx = endOrig + l;
    const lineNum = String(origLineIdx + 1).padStart(4, " ");
    diffLines.push(` ${lineNum} ${originalLines[origLineIdx]}`);
  }

  return diffLines.join("\n");
}

/*
PORT STATUS
source path: backend/agent/edit_file.go
source lines: 84
draft lines: 99
confidence: high
status: phase_b_compile
*/
