// Which mise env vars are built from others. Shared by zero:tailscale and
// flows:sandbox-host, which both move the stack to another host and have to
// re-save every template that depends on it: a mise.local.toml override of a
// root does not re-evaluate the templates mise.toml already resolved.
// No imports, so a task can use it before the workspace's node_modules exist.

// The templates in `config` that depend, directly or transitively, on any of
// `roots`, in an order where every template comes after those it reads.
export function dependentEnvVars(
  config: Record<string, unknown>,
  roots: string[],
): [string, string][] {
  const dependencies = new Map<string, string[]>();
  const unreadable = new Map<string, string[]>();
  const templates = new Map<string, string>();
  const refsIn = (text: string) =>
    [...text.matchAll(/\benv\.([A-Za-z_][A-Za-z0-9_]*)\b/g)].map((m) => m[1]);
  for (const [key, raw] of Object.entries(config)) {
    // `KEY = { default = "..." }` is a template too; any other non-string
    // value that reads another variable cannot be re-saved, so it is an error
    // once that variable turns out to be affected (checked below).
    const value =
      typeof raw === "string"
        ? raw
        : raw !== null &&
            typeof raw === "object" &&
            typeof (raw as { default?: unknown }).default === "string" &&
            Object.keys(raw).length === 1
          ? (raw as { default: string }).default
          : undefined;
    if (value === undefined) {
      const refs = refsIn(JSON.stringify(raw ?? null));
      if (refs.length > 0) unreadable.set(key, refs as string[]);
      continue;
    }
    templates.set(key, value);
    dependencies.set(key, refsIn(value) as string[]);
  }

  const affected = new Set(roots);
  let changed = true;
  while (changed) {
    changed = false;
    for (const [key, refs] of dependencies) {
      if (!affected.has(key) && refs.some((ref) => affected.has(ref))) {
        affected.add(key);
        changed = true;
      }
    }
  }

  for (const [key, refs] of unreadable) {
    if (refs.some((ref) => affected.has(ref))) {
      throw new Error(
        `${key} reads ${refs.filter((r) => affected.has(r)).join(", ")} but is not a string or { default = "..." }, so it cannot be re-saved`,
      );
    }
  }

  const visited = new Set(roots);
  const visiting = new Set<string>();
  const ordered: [string, string][] = [];
  function visit(key: string): void {
    if (visited.has(key)) return;
    if (visiting.has(key))
      throw new Error(`Cyclic environment dependency: ${key}`);
    visiting.add(key);
    for (const ref of dependencies.get(key) ?? []) {
      if (affected.has(ref)) visit(ref);
    }
    visiting.delete(key);
    visited.add(key);
    ordered.push([key, templates.get(key)!]);
  }
  for (const key of dependencies.keys()) {
    if (affected.has(key)) visit(key);
  }
  return ordered;
}
