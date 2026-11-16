// @vitest-environment node
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
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

const ROUTE_PAGES = path.join(SRC, "route-pages.tsx");
const LAZY_PATTERN =
  /const (\w+) = React\.lazy\(\s*\(\) => import\(["']([^"']+)["']\)/g;

// The modules whose components `route-pages.tsx` mounts as a page, including
// the few that live outside `src/pages`. Only imports used as a `component` or
// `indexComponent` count, so a helper imported there is not mistaken for a page.
// A loader module (route-pages.lazy.tsx) is not itself a page: follow its
// import() targets.
function pageComponents(): Set<string> {
  const source = readFileSync(ROUTE_PAGES, "utf8");
  const mounted = new Set(
    [...source.matchAll(/\b(?:component|indexComponent): (\w+)/g)].map(
      (match) => match[1]!,
    ),
  );
  const pages = new Set<string>();
  const addLazyTargets = (file: string) => {
    const lazySource = readFileSync(file, "utf8");
    let found = false;
    for (const match of lazySource.matchAll(LAZY_PATTERN)) {
      if (!mounted.has(match[1]!)) continue;
      found = true;
      const target = resolveLocal(match[2]!, file);
      if (target) pages.add(target);
    }
    return found;
  };
  addLazyTargets(ROUTE_PAGES);
  for (const match of source.matchAll(
    /import\s+(?!type\s)([^'"]*?)\s+from\s+["']([^"']+)["']/g,
  )) {
    const names = match[1]!.match(/\w+/g) ?? [];
    if (!names.some((name) => mounted.has(name))) continue;
    const resolved = resolveLocal(match[2]!, ROUTE_PAGES);
    if (!resolved) continue;
    if (!addLazyTargets(resolved)) pages.add(resolved);
  }
  return pages;
}

// Every non-test module under src that imports `route-pages.tsx`.
function routePagesImporters(): string[] {
  const importers: string[] = [];
  const visit = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== "sdk") visit(file);
      } else if (
        /\.tsx?$/.test(entry.name) &&
        !/\.test\.tsx?$/.test(entry.name)
      ) {
        const source = readFileSync(file, "utf8");
        for (const match of source.matchAll(IMPORT_PATTERN)) {
          if (resolveLocal(match[1] ?? match[2]!, file) === ROUTE_PAGES) {
            importers.push(path.relative(SRC, file));
            break;
          }
        }
      }
    }
  };
  visit(SRC);
  return importers.sort();
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

  it("keeps the route-to-page map inside the app shell", () => {
    expect(routePagesImporters()).toEqual(["App.tsx", "route-elements.tsx"]);
  });
});
