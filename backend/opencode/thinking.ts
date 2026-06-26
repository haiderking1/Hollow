import { get_reasoning, type chat_request, type message } from "./types";
import { lookup_catalog_model } from "./providers";

export type thinking_level_val = "off" | "adaptive" | "minimal" | "low" | "medium" | "high" | "max" | "xhigh" | "";

export const thinking_off: thinking_level_val = "off";
export const thinking_adaptive: thinking_level_val = "adaptive";
export const thinking_minimal: thinking_level_val = "minimal";
export const thinking_low: thinking_level_val = "low";
export const thinking_medium: thinking_level_val = "medium";
export const thinking_high: thinking_level_val = "high";
export const thinking_max: thinking_level_val = "max";
export const thinking_xhigh: thinking_level_val = "xhigh";

export type thinking_params = {
  type: string;
};

export const deepseek_v4_flash_levels: thinking_level_val[] = [
  "low",
  "medium",
  "high",
  "max"
];

export const default_reasoning_levels: thinking_level_val[] = [
  "off",
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh"
];

export const early_return_variants = (model: string): boolean => {
  const id = model.toLowerCase();
  if (id.includes("minimax")) {
    return false;
  }
  const parts = [
    "deepseek-chat",
    "deepseek-reasoner",
    "deepseek-r1",
    "deepseek-v3",
    "glm",
    "kimi",
    "k2p",
    "qwen",
    "big-pickle"
  ];
  for (const part of parts) {
    if (id.includes(part)) {
      return true;
    }
  }
  return false;
};

export const supports_thinking = (model: string): boolean => {
  return supported_thinking_levels(model).length > 1;
};

export const mandatory_thinking = (model: string): boolean => {
  return opencode_mandatory_thinking_id(model);
};

export const opencode_mandatory_thinking_id = (id: string): boolean => {
  const lower_id = id.toLowerCase();
  if (
    lower_id.includes("deepseek-chat") ||
    lower_id.includes("deepseek-reasoner") ||
    lower_id.includes("deepseek-r1") ||
    lower_id.includes("deepseek-v3")
  ) {
    return true;
  }
  const parts = ["glm", "kimi", "k2p", "qwen", "big-pickle"];
  for (const part of parts) {
    if (lower_id.includes(part)) {
      return true;
    }
  }
  return false;
};

export const is_minimax_model = (model: string): boolean => model.toLowerCase().includes("minimax");

export const is_minimax_m3 = (model: string): boolean => model.toLowerCase().includes("minimax-m3");

export const supported_thinking_levels = (model: string): thinking_level_val[] => {
  const model_lower = model.toLowerCase();
  if (model_lower.includes("gpt-")) {
    return [...default_reasoning_levels];
  }
  if (is_minimax_m3(model_lower)) {
    // MiniMax-M3: thinking.type is "disabled" or "adaptive" — not reasoning_effort tiers.
    return ["off", "adaptive"];
  }
  if (is_minimax_model(model_lower)) {
    // M2.x: thinking is always on and cannot be disabled.
    return [];
  }
  if (early_return_variants(model)) {
    if (mandatory_thinking(model)) {
      return ["medium"];
    }
    return ["off"];
  }
  if (model_lower.includes("deepseek-v4")) {
    return [...deepseek_v4_flash_levels];
  }
  // Unknown models: do not assume thinking support. The resolver will still
  // flag well-known families above, and users can override via ~/.hollow/models.json.
  return [];
};

export const cycle_thinking_level = (current: thinking_level_val, model: string): thinking_level_val => {
  const levels = supported_thinking_levels(model);
  if (levels.length <= 1) {
    return "off";
  }
  let idx = 0;
  for (let i = 0; i < levels.length; i++) {
    if (levels[i] === current) {
      idx = i;
      break;
    }
  }
  return levels[(idx + 1) % levels.length];
};

export const step_thinking_level = (current: thinking_level_val, model: string, delta: number): thinking_level_val => {
  const levels = supported_thinking_levels(model);
  if (levels.length <= 1) {
    return "off";
  }
  let idx = 0;
  for (let i = 0; i < levels.length; i++) {
    if (levels[i] === current) {
      idx = i;
      break;
    }
  }
  const n = levels.length;
  idx = ((idx + delta) % n + n) % n;
  return levels[idx];
};

export const normalize_thinking_level = (level: string, model: string): thinking_level_val => {
  const parsed = parse_thinking_level(level);
  if (is_minimax_m3(model) && (parsed === "medium" || level === "thinking")) {
    return "adaptive";
  }
  return parsed;
};

export const default_thinking_level = (model: string): thinking_level_val => {
  const levels = supported_thinking_levels(model);
  if (levels.length === 0) {
    return "off";
  }
  if (levels.includes("adaptive")) {
    return "adaptive";
  }
  if (levels.includes("medium")) {
    return "medium";
  }
  return levels.find((l) => l !== "off") ?? levels[0];
};

const apply_minimax_thinking = (req: chat_request, level: thinking_level_val, model: string): void => {
  const normalized = normalize_thinking_level(level, model);
  if (is_minimax_m3(model)) {
    if (normalized === "off") {
      req.thinking = { type: "disabled" };
    } else {
      // Empty level means "use API default" (adaptive on); do not send disabled.
      req.thinking = { type: "adaptive" };
    }
  } else {
    req.thinking = { type: "adaptive" };
  }
  // MiniMax only streams reasoning_content / reasoning_details when this is set.
  req.reasoning_split = true;
  delete req.reasoning_effort;
};

export const apply_thinking_to_request = (req: chat_request, level: thinking_level_val, model: string): void => {
  if (is_minimax_model(model)) {
    apply_minimax_thinking(req, level, model);
    return;
  }
  if (!supports_thinking(model)) {
    return;
  }

  if (level === "off" || level === "") {
    delete req.reasoning_effort;
    delete req.thinking;
    return;
  }

  const effort = reasoning_effort_for_api(level, model);
  req.reasoning_effort = effort;
  delete req.thinking;
};

export const reasoning_effort_for_api = (level: thinking_level_val, model: string): string => {
  if (level === "off" || level === "") {
    return "";
  }
  if (model.toLowerCase().includes("deepseek-v4")) {
    if (level === "xhigh" || level === "max") {
      return "max";
    }
    return level;
  }
  if (level === "max") {
    return "xhigh";
  }
  return level;
};

export const supports_reasoning = (model: string): boolean => {
  const [m, ok] = lookup_catalog_model(model);
  if (ok) {
    return m.reasoning || (m.reasoning_field !== undefined && m.reasoning_field !== "");
  }
  const model_lower = model.toLowerCase();
  if (opencode_mandatory_thinking_id(model)) {
    return true;
  }
  if (
    model_lower.includes("deepseek-v4") ||
    model_lower.includes("deepseek-r1") ||
    model_lower.includes("reasoner")
  ) {
    return true;
  }
  if (model_lower.includes("mimo") || model_lower.includes("hy3")) {
    return true;
  }
  if (model_lower.includes("gpt-")) {
    return true;
  }
  if (is_minimax_model(model)) {
    return true;
  }
  return false;
};

export const normalize_messages = (msgs: message[], model: string): message[] => {
  if (!supports_reasoning(model)) {
    return msgs;
  }
  let field = "reasoning_content";
  const [m, ok] = lookup_catalog_model(model);
  if (ok && m.reasoning_field !== undefined && m.reasoning_field !== "") {
    field = m.reasoning_field;
  }

  return msgs.map((msg) => {
    if (msg.role !== "assistant") {
      return msg;
    }
    const reasoning_text = get_reasoning(msg);
    const next_msg = { ...msg };
    delete next_msg.reasoning_content;
    delete next_msg.reasoning_details;
    delete next_msg.reasoning;

    switch (field) {
      case "reasoning_details":
        next_msg.reasoning_details = reasoning_text;
        break;
      case "reasoning":
        next_msg.reasoning = reasoning_text;
        break;
      default:
        next_msg.reasoning_content = reasoning_text;
    }
    return next_msg;
  });
};

export const parse_thinking_level = (s: string): thinking_level_val => {
  switch (s) {
    case "adaptive":
    case "thinking":
      return "adaptive";
    case "minimal":
    case "low":
    case "medium":
    case "high":
    case "max":
    case "xhigh":
      return s;
    default:
      return "off";
  }
};

export const format_thinking_label = (level: thinking_level_val): string => {
  if (level === "" || level === "off") {
    return "off";
  }
  return level;
};

export const format_thinking_level_for_model = (model: string, level: thinking_level_val): string => {
  if (is_minimax_m3(model)) {
    if (level === "off" || level === "") {
      return "off";
    }
    if (level === "adaptive" || level === "medium") {
      return "thinking";
    }
  }
  return format_thinking_label(level);
};

