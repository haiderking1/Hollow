// PORT: backend/config/common errors

export type config_error = {
  readonly _tag: "ConfigError";
  readonly reason: string;
  readonly cause: unknown;
};

export const config_error = (reason: string, cause: unknown): config_error => ({
  _tag: "ConfigError",
  reason,
  cause,
});
