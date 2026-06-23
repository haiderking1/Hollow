// PORT: backend/auth/common errors

export type auth_error = {
  readonly _tag: "AuthError";
  readonly reason: string;
  readonly cause: unknown;
};

export const auth_error = (reason: string, cause: unknown): auth_error => ({
  _tag: "AuthError",
  reason,
  cause,
});
