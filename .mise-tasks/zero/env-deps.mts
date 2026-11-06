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
  for (const [key, value] of Object.entries(config)) {
    if (typeof value !== "string") continue;
    dependencies.set(
      key,
      [...value.matchAll(/\benv\.([A-Za-z_][A-Za-z0-9_]*)\b/g)].map(
        (match) => match[1],
      ),
    );
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
    ordered.push([key, config[key] as string]);
  }
  for (const key of dependencies.keys()) {
    if (affected.has(key)) visit(key);
  }
  return ordered;
}
