export type ShadowServerFacts = {
  transport: string;
  pkg: string | null;
  version: string | null;
};

// Launcher binaries and their subcommands, skipped when looking for the
// package an stdio command runs.
const LAUNCHERS = new Set([
  "npx",
  "bunx",
  "uvx",
  "pnpm",
  "dlx",
  "yarn",
  "node",
  "python",
  "python3",
  "uv",
  "run",
  "docker",
]);

function splitVersion(token: string): { pkg: string; version: string | null } {
  const pip = token.indexOf("==");
  if (pip > 0)
    return { pkg: token.slice(0, pip), version: token.slice(pip + 2) || null };
  // The first "@" of a scoped npm package is part of its name.
  const at = token.lastIndexOf("@");
  if (at > 0)
    return { pkg: token.slice(0, at), version: token.slice(at + 1) || null };
  return { pkg: token, version: null };
}

/** Best-effort facts read off a shadow MCP server identifier (URL or command). */
export function shadowServerFacts(identifier: string): ShadowServerFacts {
  const trimmed = identifier.trim();
  if (/^https?:\/\//i.test(trimmed)) {
    try {
      return { transport: "http", pkg: new URL(trimmed).host, version: null };
    } catch {
      return { transport: "http", pkg: null, version: null };
    }
  }
  const token = trimmed
    .split(/\s+/)
    .find((t) => t && !t.startsWith("-") && !LAUNCHERS.has(t));
  if (!token) return { transport: "stdio", pkg: null, version: null };
  return { transport: "stdio", ...splitVersion(token) };
}
