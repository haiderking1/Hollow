// PORT: backend/auth/codex_headers.go

// CodexCloudflareHeaders returns headers required for chatgpt.com/backend-api/codex.
export const codex_cloudflare_headers = (access_token: string): Record<string, string> => {
  const headers: Record<string, string> = {
    "User-Agent": "codex_cli_rs/0.0.0 (Hollow)",
    originator: "codex_cli_rs",
  };

  access_token = access_token.trim();
  if (access_token === "") {
    return headers;
  }

  const parts = access_token.split(".");
  if (parts.length < 2) {
    return headers;
  }

  let payload_b64 = parts[1];
  payload_b64 += "=".repeat((4 - (payload_b64.length % 4)) % 4);
  payload_b64 = payload_b64.replace(/-/g, "+").replace(/_/g, "/");

  let raw: Uint8Array;
  try {
    raw = new Uint8Array(Buffer.from(payload_b64.replace(/=+$/, ""), "base64url"));
  } catch {
    try {
      raw = new Uint8Array(Buffer.from(payload_b64, "base64"));
    } catch {
      return headers;
    }
  }

  const claims = JSON.parse(new TextDecoder().decode(raw)) as {
    "https://api.openai.com/auth"?: { chatgpt_account_id?: string };
  };

  const id = (claims["https://api.openai.com/auth"]?.chatgpt_account_id ?? "").trim();
  if (id !== "") {
    headers["ChatGPT-Account-ID"] = id;
  }
  return headers;
};

/*
PORT STATUS
source path: backend/auth/codex_headers.go
source lines: 43
draft lines: 57
confidence: high
status: phase_a_draft
todos:
  - confirm base64url fallback ordering matches Go's RawURLEncoding + URLEncoding
notes:
  - No (T, error) returns; pure function port.
*/
