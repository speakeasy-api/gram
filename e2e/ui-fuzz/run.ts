import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// One run's identity and directory, shared by the recorder, the fuzz spec
// and the summary so they agree on where records go. The mise task sets
// UI_FUZZ_RUN_ID once so all three steps see the same run.

export const repoRoot = path.resolve(import.meta.dirname, "..", "..");
export const fuzzRoot = path.join(repoRoot, "e2e", ".ui-fuzz");

export const commit: string =
  process.env["GITHUB_SHA"] ??
  execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: repoRoot,
    encoding: "utf8",
  }).trim();

export const runId: string =
  process.env["UI_FUZZ_RUN_ID"] ??
  `${new Date().toISOString().replace(/[-:]/g, "").slice(0, 15)}-${commit.slice(0, 8)}`;

export const runDir = path.join(fuzzRoot, runId);

export function ensureRunDir(): string {
  fs.mkdirSync(path.join(runDir, "snapshots"), { recursive: true });
  return runDir;
}

// The recorder's output: which rpc calls each static route makes. Written
// next to the runs rather than inside one, so the latest recording serves
// a fuzz run started on its own.
export const depsFile = path.join(fuzzRoot, "deps.json");

export type RouteDeps = {
  key: string;
  path: string;
  title: string;
  // Where the browser ended up; differs from `path` when the route redirects.
  final_path: string;
  rpcs: string[];
};

export type Deps = {
  commit: string;
  recorded_at: string;
  routes: Record<string, RouteDeps>;
};

export function readDeps(): Deps | undefined {
  if (!fs.existsSync(depsFile)) return undefined;
  return JSON.parse(fs.readFileSync(depsFile, "utf8")) as Deps;
}

// RPCs the app shell makes on most routes (session, project, flags). A
// mutation there breaks every screen the same way, which says nothing about
// the screen under test, so they are excluded from the per-route set.
export const FRAME_SHARE = 0.5;

export function frameRpcs(deps: Deps): Set<string> {
  const routes = Object.values(deps.routes);
  const counts = new Map<string, number>();
  for (const r of routes) {
    for (const rpc of new Set(r.rpcs))
      counts.set(rpc, (counts.get(rpc) ?? 0) + 1);
  }
  const frame = new Set<string>();
  for (const [rpc, n] of counts) {
    if (routes.length > 0 && n / routes.length > FRAME_SHARE) frame.add(rpc);
  }
  return frame;
}
