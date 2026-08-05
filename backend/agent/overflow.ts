// Pure overflow-error detection — no Agent import, no side effects.

export const overflowPatterns = [
  /prompt is too long/i,
  /request_too_large/i,
  /input is too long for requested model/i,
  /exceeds the context window/i,
  /exceeds (?:the )?(?:model'?s )?maximum context length/i,
  /input token count.*exceeds the maximum/i,
  /maximum prompt length is \d+/i,
  /reduce the length of the messages/i,
  /maximum context length is \d+ tokens/i,
  /input \(\d+ tokens\) is longer than the model'?s context length/i,
  /exceeds the limit of \d+/i,
  /exceeds the available context size/i,
  /greater than the context length/i,
  /context window exceeds limit/i,
  /exceeded model token limit/i,
  /too large for model with \d+ maximum context length/i,
  /model_context_window_exceeded/i,
  /prompt too long; exceeded (?:max )?context length/i,
  /context[_ ]length[_ ]exceeded/i,
  /too many tokens/i,
  /token limit exceeded/i,
];

export function IsContextOverflowError(err: Error | null | undefined): boolean {
  if (!err) {
    return false;
  }
  const messages: string[] = [];
  const seen = new Set<unknown>();
  let current: unknown = err;
  while (current && !seen.has(current)) {
    seen.add(current);
    if (current instanceof Error) {
      messages.push(current.message);
      current = current.cause;
      continue;
    }
    if (typeof current === "object") {
      const value = current as { reason?: unknown; message?: unknown; cause?: unknown };
      if (typeof value.reason === "string") messages.push(value.reason);
      if (typeof value.message === "string") messages.push(value.message);
      current = value.cause;
      continue;
    }
    messages.push(String(current));
    break;
  }
  const msg = messages.join(" ");
  for (const p of overflowPatterns) {
    if (p.test(msg)) {
      return true;
    }
  }
  return false;
}
