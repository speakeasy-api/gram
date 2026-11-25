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

const DOCKER_VALUE_OPTIONS: ReadonlySet<string> = new Set([
  "-v",
  "--volume",
  "-e",
  "--env",
  "--env-file",
  "--name",
  "--network",
  "--net",
  "-p",
  "--publish",
  "--mount",
  "--entrypoint",
  "-w",
  "--workdir",
  "-u",
  "--user",
  "-l",
  "--label",
  "-h",
  "--hostname",
  "-m",
  "--memory",
  "--platform",
  "--add-host",
  "--cpus",
  "--pull",
  "--restart",
]);

const UV_VALUE_OPTIONS: ReadonlySet<string> = new Set([
  "--from",
  "--with",
  "--python",
  "--index-url",
]);

// Options whose value is the next token, per launcher binary, so the value
// isn't read as the package.
const LAUNCHER_VALUE_OPTIONS: Record<string, ReadonlySet<string> | undefined> =
  {
    docker: DOCKER_VALUE_OPTIONS,
    uvx: UV_VALUE_OPTIONS,
    uv: UV_VALUE_OPTIONS,
  };

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
  const token = packageToken(trimmed.split(/\s+/));
  if (!token) return { transport: "stdio", pkg: null, version: null };
  return { transport: "stdio", ...splitVersion(token) };
}

function packageToken(tokens: string[]): string | undefined {
  let valueOptions: ReadonlySet<string> | undefined;
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i]!;
    if (!t) continue;
    if (t.startsWith("-")) {
      // An `--opt=value` form carries its value in the same token.
      if (!t.includes("=") && valueOptions?.has(t)) i++;
      continue;
    }
    if (!LAUNCHERS.has(t)) return t;
    valueOptions ??= LAUNCHER_VALUE_OPTIONS[t];
  }
  return undefined;
}
