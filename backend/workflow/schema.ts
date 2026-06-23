// PORT: backend/workflow/schema.go

import { Effect } from "effect";
import Ajv from "ajv";

export function stripJSONFences(text: string): string {
  let s = text.trim();
  const idx = s.indexOf("```");
  if (idx >= 0) {
    s = s.slice(idx);
    if (s.startsWith("```")) {
      const nl = s.indexOf("\n");
      if (nl >= 0) {
        s = s.slice(nl + 1);
      }
      const end = s.lastIndexOf("```");
      if (end >= 0) {
        s = s.slice(0, end);
      }
    }
  }

  const start = s.search(/[{[]/);
  if (start > 0) {
    s = s.slice(start);
  }

  while (s.length > 0) {
    try {
      JSON.parse(s);
      break;
    } catch {
      const lastBrace = s.lastIndexOf("}");
      const lastBracket = s.lastIndexOf("]");
      const end = Math.max(lastBrace, lastBracket);
      if (end >= 0 && end + 1 < s.length) {
        s = s.slice(0, end + 1).trim();
        continue;
      }
      break;
    }
  }
  return s.trim();
}

export function parseAndValidateJSON(
  text: string,
  schemaDoc: Record<string, any>
): Effect.Effect<any, Error> {
  return Effect.try({
    try: () => {
      const stripped = stripJSONFences(text);
      let value: any;
      try {
        value = JSON.parse(stripped);
      } catch (err: any) {
        throw new Error(`invalid JSON: ${err.message}`);
      }

      if (!schemaDoc || Object.keys(schemaDoc).length === 0) {
        return value;
      }

      const ajv = new Ajv();
      let validate;
      try {
        validate = ajv.compile(schemaDoc);
      } catch (err: any) {
        throw new Error(`invalid response schema: ${err.message}`);
      }

      const valid = validate(value);
      if (!valid) {
        const errors = validate.errors
          ?.map((e) => `${e.instancePath || "/"} ${e.message}`)
          .join(", ") || "unknown schema validation error";
        throw new Error(`response does not match schema: ${errors}`);
      }

      return value;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/workflow/schema.go
source lines: 64
draft lines: 90
confidence: high
status: phase_b_compile
*/
