import fs from "node:fs";
import path from "node:path";

import {
  loadCatalogue,
  resolvePath,
  staticRoutes,
} from "../ui-fuzz/catalogue.ts";
import { expect, test } from "../ui-fuzz/fixtures.ts";
import {
  commit,
  depsFile,
  ensureRunDir,
  runDir,
  type Deps,
  type RouteDeps,
} from "../ui-fuzz/run.ts";

// Step one of a fuzz run: visit every static route unmutated and record
// which rpc calls it makes. The fuzz spec reads the result to know what to
// mutate on each screen, so nobody keeps a per-page dependency list by
// hand. No judge is called here.

const routes = staticRoutes(loadCatalogue());
const recorded: RouteDeps[] = [];

for (const route of routes) {
  test(`record ${route.key}`, async ({ page, session }) => {
    const target = resolvePath(route, session.orgSlug, session.projectSlug);
    const rpcs = new Set<string>();
    page.on("response", (response) => {
      const request = response.request();
      const url = new URL(request.url());
      if (request.method() !== "GET" || !url.pathname.startsWith("/rpc/"))
        return;
      if (response.status() !== 200) return;
      if (!(response.headers()["content-type"] ?? "").includes("json")) return;
      rpcs.add(url.pathname);
    });
    await page.goto(target);
    await page
      .waitForLoadState("networkidle", { timeout: 15_000 })
      .catch(() => undefined);
    await page.waitForTimeout(1_000);
    const finalPath = new URL(page.url()).pathname;
    recorded.push({
      key: route.key,
      path: route.path,
      title: route.title,
      final_path: finalPath,
      rpcs: [...rpcs].sort(),
    });
    test.info().annotations.push({
      type: "rpcs",
      description: [...rpcs].join(", ") || "(none)",
    });
    // A route that bounced somewhere else is still recorded, with where it
    // went, so the fuzz step can skip it with a reason instead of judging
    // the wrong screen.
    expect(finalPath, `${route.key} redirected`).toBe(target);
  });
}

test.afterAll(() => {
  // Workers each hold their own slice; merge into whatever is on disk from
  // the other workers of this same run.
  ensureRunDir();
  const partial = path.join(runDir, `deps-w${test.info().workerIndex}.json`);
  fs.writeFileSync(partial, JSON.stringify(recorded, null, 2));
  const merged: Deps = {
    commit,
    recorded_at: new Date().toISOString(),
    routes: {},
  };
  for (const file of fs
    .readdirSync(runDir)
    .filter((f) => /^deps-w\d+\.json$/.test(f))) {
    const slice = JSON.parse(
      fs.readFileSync(path.join(runDir, file), "utf8"),
    ) as RouteDeps[];
    for (const r of slice) merged.routes[r.key] = r;
  }
  fs.writeFileSync(depsFile, JSON.stringify(merged, null, 2) + "\n");
  fs.writeFileSync(
    path.join(runDir, "deps.json"),
    JSON.stringify(merged, null, 2) + "\n",
  );
});
