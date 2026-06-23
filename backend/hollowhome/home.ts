import path from "node:path";

/** Hollow home directory (default: ~/.hollow). Override with HOLLOW_HOME. */
export const home_dir = (): string => {
  const override = process.env.HOLLOW_HOME;
  if (override !== undefined && override !== "") {
    return override;
  }

  const home = process.env.HOME ?? process.env.USERPROFILE;
  if (home === undefined || home === "") {
    return ".hollow";
  }

  return path.join(home, ".hollow");
};
