import { Effect } from "effect";
import { client } from "../opencode/client";
import { string_content, content_string, type chat_request } from "../opencode/types";

// Title generation is background-only, so allow a little provider queue time
// without ever delaying the real assistant response.
export const TITLE_TIMEOUT_MS = 8000;
export const TITLE_MAX_TOKENS = 20;

export const pendingOrCompletedTitleSessions = new Set<string>();

export function clearPendingTitleSessions(): void {
  pendingOrCompletedTitleSessions.clear();
}

export function sanitizeTitle(raw: string): string {
  if (!raw) return "";
  let clean = raw.trim();

  // Strip markdown code blocks
  clean = clean.replace(/```[\s\S]*?```/g, "").trim();

  // Replace newlines with spaces
  clean = clean.replace(/[\r\n]+/g, " ");

  // Strip markdown headers e.g. # Title
  clean = clean.replace(/^#+\s*/, "");

  // Strip common prefixes like "Title:", "Topic:", "Session Title:"
  clean = clean.replace(/^(title|topic|session title)\s*:\s*/i, "");

  // Strip wrapping quotes or backticks
  clean = clean.replace(/^["'`]+|["'`]+$/g, "").trim();

  // Strip inline markdown formatters (*, _, ~, `)
  clean = clean.replace(/[*_~`]/g, "");

  // Collapse extra spaces
  clean = clean.replace(/\s+/g, " ").trim();

  // Cap maximum length (e.g. 60 chars)
  if (clean.length > 60) {
    clean = clean.slice(0, 60).trim();
    const lastSpace = clean.lastIndexOf(" ");
    if (lastSpace > 10) {
      clean = clean.slice(0, lastSpace).trim();
    }
  }

  return clean;
}

export const generateSessionTitle = (
  chatClient: client,
  userText: string,
  assistantText?: string,
  timeoutMs = TITLE_TIMEOUT_MS,
): Effect.Effect<string, Error> => {
  return Effect.tryPromise({
    try: async () => {
      const cleanUser = userText.trim().slice(0, 500);
      if (cleanUser === "") return "";

      let promptText = `Summarize the user request into a concise 3 to 6 word session title.\nUser request: "${cleanUser}"`;
      if (assistantText) {
        const cleanAsst = assistantText.trim().slice(0, 300);
        if (cleanAsst !== "") {
          promptText += `\nAssistant response start: "${cleanAsst}"`;
        }
      }
      promptText += `\n\nOutput ONLY the plain title text. Do NOT use quotes, markdown, linebreaks, or prefixes.`;

      const titleClient = new client(chatClient.base_url, chatClient.api_key, chatClient.model);
      Object.assign(titleClient, chatClient);
      titleClient.timeout_ms = timeoutMs;

      const req: chat_request = {
        model: chatClient.model,
        messages: [
          { role: "user", content: string_content(promptText) },
        ],
        max_tokens: TITLE_MAX_TOKENS,
        // Titles must be fast and must come back in `content`, not consume the
        // tiny output budget on hidden reasoning tokens.
        thinking: { type: "disabled" },
      };

      const controller = new AbortController();
      const result = await Effect.runPromise(Effect.either(titleClient.chat(controller.signal, req)));

      if (result._tag === "Left") {
        return "";
      }

      const choices = result.right.choices;
      if (!choices || choices.length === 0) return "";

      const raw = content_string(choices[0].message);
      return sanitizeTitle(raw);
    },
    catch: () => new Error("Title generation failed"),
  });
};
