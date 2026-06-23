// PORT: backend/workflow/roles.go

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

let currentDir = "";
try {
  currentDir = path.dirname(fileURLToPath(import.meta.url));
} catch {
  currentDir = __dirname;
}

export function roleTemplate(role: string): string {
  role = role.trim().toLowerCase();
  if (role === "") {
    role = "audit";
  }
  try {
    const data = fs.readFileSync(path.join(currentDir, "roles", `${role}.txt`), "utf8");
    const now = new Date();
    const year = now.getFullYear();
    const month = String(now.getMonth() + 1).padStart(2, "0");
    const day = String(now.getDate()).padStart(2, "0");
    const today = `${year}-${month}-${day}`;
    return data.replace(/\{\{today\}\}/g, today);
  } catch {
    return `You are the ${role} subagent in a dynamic workflow. Follow the prompt exactly and return only the requested result.`;
  }
}

/*
PORT STATUS
source path: backend/workflow/roles.go
source lines: 24
draft lines: 28
confidence: high
status: phase_b_compile
*/
