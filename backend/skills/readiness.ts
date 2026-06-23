// PORT: backend/skills/readiness.go

import fs from "node:fs";
import path from "node:path";
import { HomeDir } from "./paths";
import { toStringList } from "./frontmatter";

export type SkillReadinessStatus = "available" | "setup_needed" | "unsupported";

export const ReadinessAvailable: SkillReadinessStatus = "available";
export const ReadinessSetupNeeded: SkillReadinessStatus = "setup_needed";
export const ReadinessUnsupported: SkillReadinessStatus = "unsupported";

export interface RequiredEnvVar {
  Name: string;
  Prompt: string;
  Help?: string;
  RequiredFor?: string;
  Optional?: boolean;
}

export interface CollectSecretEntry {
  EnvVar: string;
  Prompt: string;
  ProviderURL?: string;
  Secret: boolean;
}

export interface SetupBlock {
  Help: string | null;
  CollectSecrets: CollectSecretEntry[];
}

const envVarNameRe = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function LoadHollowEnv(): Record<string, string> {
  const envMap: Record<string, string> = {};
  const home = HomeDir();
  const envPath = path.join(home, ".env");
  let data: string;
  try {
    data = fs.readFileSync(envPath, "utf8");
  } catch {
    return envMap;
  }
  const lines = data.split("\n");
  for (let line of lines) {
    line = line.trim();
    if (line === "" || line.startsWith("#")) {
      continue;
    }
    const parts = line.split("=");
    if (parts.length >= 2) {
      const k = parts[0].trim();
      let v = parts.slice(1).join("=").trim();
      v = v.replace(/^["']|["']$/g, ""); // Strip leading/trailing quotes
      envMap[k] = v;
    }
  }
  return envMap;
}

export function isEnvVarSet(name: string, envMap: Record<string, string>): boolean {
  if (process.env[name] !== undefined && process.env[name] !== "") {
    return true;
  }
  if (envMap[name] !== undefined && envMap[name] !== "") {
    return true;
  }
  return false;
}

export function normalizeSetupMetadata(fm: Record<string, any>): SetupBlock {
  const setupVal = fm["setup"];
  if (setupVal === undefined || setupVal === null) {
    return { Help: null, CollectSecrets: [] };
  }
  const setupMap = setupVal as Record<string, any>;
  if (typeof setupMap !== "object") {
    return { Help: null, CollectSecrets: [] };
  }

  let helpText: string | null = null;
  if (typeof setupMap["help"] === "string" && setupMap["help"].trim() !== "") {
    helpText = setupMap["help"].trim();
  }

  const collectSecrets: CollectSecretEntry[] = [];
  const csVal = setupMap["collect_secrets"];
  if (csVal !== undefined && csVal !== null) {
    let rawList: any[] = [];
    if (Array.isArray(csVal)) {
      rawList = csVal;
    } else if (typeof csVal === "object") {
      rawList = [csVal];
    }

    for (const item of rawList) {
      if (!item || typeof item !== "object") {
        continue;
      }
      let envVar = "";
      if (typeof item["env_var"] === "string") {
        envVar = item["env_var"].trim();
      }
      if (envVar === "") {
        continue;
      }
      let prompt = `Enter value for ${envVar}`;
      if (typeof item["prompt"] === "string" && item["prompt"].trim() !== "") {
        prompt = item["prompt"].trim();
      }
      let providerURL = "";
      if (typeof item["provider_url"] === "string") {
        providerURL = item["provider_url"].trim();
      } else if (typeof item["url"] === "string") {
        providerURL = item["url"].trim();
      }

      let secret = true;
      if (item["secret"] !== undefined) {
        if (typeof item["secret"] === "boolean") {
          secret = item["secret"];
        }
      }
      collectSecrets.push({
        EnvVar: envVar,
        Prompt: prompt,
        ProviderURL: providerURL || undefined,
        Secret: secret,
      });
    }
  }

  return {
    Help: helpText,
    CollectSecrets: collectSecrets,
  };
}

export function getRequiredEnvironmentVariables(fm: Record<string, any>): RequiredEnvVar[] {
  const setup = normalizeSetupMetadata(fm);
  const required: RequiredEnvVar[] = [];
  const seen = new Set<string>();

  const appendRequired = (entry: Record<string, any>) => {
    let name = "";
    if (typeof entry["name"] === "string") {
      name = entry["name"].trim();
    } else if (typeof entry["env_var"] === "string") {
      name = entry["env_var"].trim();
    }
    if (name === "" || seen.has(name)) {
      return;
    }
    if (!envVarNameRe.test(name)) {
      return;
    }

    let prompt = `Enter value for ${name}`;
    if (typeof entry["prompt"] === "string" && entry["prompt"].trim() !== "") {
      prompt = entry["prompt"].trim();
    }

    let helpText = "";
    if (typeof entry["help"] === "string" && entry["help"].trim() !== "") {
      helpText = entry["help"].trim();
    } else if (typeof entry["provider_url"] === "string" && entry["provider_url"].trim() !== "") {
      helpText = entry["provider_url"].trim();
    } else if (typeof entry["url"] === "string" && entry["url"].trim() !== "") {
      helpText = entry["url"].trim();
    } else if (setup.Help !== null) {
      helpText = setup.Help;
    }

    let requiredFor = "";
    if (typeof entry["required_for"] === "string") {
      requiredFor = entry["required_for"].trim();
    }

    let optional = false;
    if (typeof entry["optional"] === "boolean") {
      optional = entry["optional"];
    }

    seen.add(name);
    required.push({
      Name: name,
      Prompt: prompt,
      Help: helpText || undefined,
      RequiredFor: requiredFor || undefined,
      Optional: optional,
    });
  };

  // 1. required_environment_variables
  const reqRaw = fm["required_environment_variables"];
  if (reqRaw !== undefined && reqRaw !== null) {
    let rawList: any[] = [];
    if (typeof reqRaw === "string") {
      rawList = [reqRaw];
    } else if (Array.isArray(reqRaw)) {
      rawList = reqRaw;
    } else if (typeof reqRaw === "object") {
      rawList = [reqRaw];
    }
    for (const item of rawList) {
      if (typeof item === "string") {
        appendRequired({ name: item });
      } else if (item && typeof item === "object") {
        appendRequired(item);
      }
    }
  }

  // 2. setup.collect_secrets
  for (const item of setup.CollectSecrets) {
    appendRequired({
      name: item.EnvVar,
      prompt: item.Prompt,
      provider_url: item.ProviderURL,
    });
  }

  // 3. legacy prerequisites.env_vars
  const prereqs = fm["prerequisites"];
  if (prereqs && typeof prereqs === "object") {
    const envVarsVal = prereqs["env_vars"];
    if (envVarsVal !== undefined && envVarsVal !== null) {
      let list: string[] = [];
      if (typeof envVarsVal === "string" && envVarsVal.trim() !== "") {
        list = [envVarsVal.trim()];
      } else if (Array.isArray(envVarsVal)) {
        list = toStringList(envVarsVal);
      }
      for (const ev of list) {
        appendRequired({ name: ev });
      }
    }
  }

  return required;
}

export function CheckSkillsRequirements(): boolean {
  return true;
}

/*
PORT STATUS
source path: backend/skills/readiness.go
source lines: 256
draft lines: 255
confidence: high
status: phase_b_compile
*/
