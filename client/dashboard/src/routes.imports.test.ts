// @vitest-environment node
import { existsSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// `@/routes` is imported by hundreds of shared components to build links. If
// it reaches a page module, every test that renders one of those components
// loads the whole page tree. Pages are mapped to routes in `route-pages.tsx`,
// which only the app shell imports.

const SRC = path.resolve(import.meta.dirname);
const PAGES = path.join(SRC, "pages") + path.sep;
const EXTENSIONS = [".tsx", ".ts", "/index.tsx", "/index.ts"];

// Static imports, re-exports and dynamic imports. Type-only imports are
// erased at build time, so they do not load anything.
const IMPORT_PATTERN =
  /(?:import|export)\s+(?!type\s)(?:[^'"]*?\sfrom\s+)?["']([^"']+)["']|import\(\s*["']([^"']+)["']\s*\)/g;

function resolveLocal(specifier: string, fromFile: string): string | null {
  let base: string;
  if (specifier.startsWith("@/")) {
    base = path.join(SRC, specifier.slice(2));
  } else if (specifier.startsWith(".")) {
    base = path.resolve(path.dirname(fromFile), specifier);
  } else {
    return null;
  }
  if (existsSync(base) && statSync(base).isFile()) return base;
  for (const extension of EXTENSIONS) {
    if (existsSync(base + extension)) return base + extension;
  }
  return null;
}

function reachableModules(entry: string): Map<string, string> {
  // Each reachable module, keyed by path, mapped to the module that imported it.
  const importers = new Map<string, string>([[entry, ""]]);
  const queue = [entry];
  while (queue.length > 0) {
    const file = queue.pop()!;
    const source = readFileSync(file, "utf8");
    for (const match of source.matchAll(IMPORT_PATTERN)) {
      const resolved = resolveLocal(match[1] ?? match[2]!, file);
      if (resolved && !importers.has(resolved)) {
        importers.set(resolved, file);
        queue.push(resolved);
      }
    }
  }
  return importers;
}

function importChain(importers: Map<string, string>, file: string): string {
  const chain = [];
  for (let current = file; current; current = importers.get(current)!) {
    chain.unshift(path.relative(SRC, current));
  }
  return chain.join(" -> ");
}

// The modules `route-pages.tsx` imports directly: every component mounted as a
// page, including the few that live outside `src/pages`.
function pageComponents(): Set<string> {
  const routePages = path.join(SRC, "route-pages.tsx");
  const source = readFileSync(routePages, "utf8");
  const pages = new Set<string>();
  for (const match of source.matchAll(IMPORT_PATTERN)) {
    const resolved = resolveLocal(match[1] ?? match[2]!, routePages);
    if (resolved) pages.add(resolved);
  }
  return pages;
}

describe("@/routes import graph", () => {
  it("does not reach any page module", () => {
    const importers = reachableModules(path.join(SRC, "routes.tsx"));
    const pages = pageComponents();
    const pageChains = [...importers.keys()]
      .filter((file) => file.startsWith(PAGES) || pages.has(file))
      .map((file) => importChain(importers, file));

    expect(pageChains).toEqual([]);
  });
});
