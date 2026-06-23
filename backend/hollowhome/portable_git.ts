import path from "node:path";
import { home_dir } from "./home";

/** Portable Git install path on Windows. */
export const portable_git_dir = (): string => {
  const la = process.env.LOCALAPPDATA;
  if (la !== undefined && la !== "") {
    return path.join(la, "hollow", "git");
  }

  return path.join(home_dir(), "git");
};
