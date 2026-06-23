// PORT: backend/web/searxng/common errors

export type searxng_error = {
  readonly _tag: "SearxngError";
  readonly reason: string;
  readonly cause: unknown;
};

export const searxng_error = (reason: string, cause: unknown): searxng_error => ({
  _tag: "SearxngError",
  reason,
  cause,
});
