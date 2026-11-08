import assert from "node:assert/strict";
import { test } from "node:test";

import {
  loadCatalogue,
  parseCatalogue,
  resolvePath,
  staticRoutes,
} from "./catalogue.ts";

test("parses a small table with nesting, flags, comments and JSX", () => {
  const src = `
const X = 1;
const ROUTE_STRUCTURE = {
  login: { title: "Login", url: "/login", component: Login, unauthenticated: true },
  home: {
    // "Home" {braces in a comment}
    title: "Project Overview",
    url: "",
    component: () => <Navigate to="/x" replace />,
  },
  mcp: {
    title: "MCP",
    url: "mcp",
    icon: "network",
    component: () => { const y = { a: 1 }; return y; },
    subPages: {
      add: { title: "Add", url: "add", subPages: { remote: { title: "Remote", url: "remote" } } },
      details: { title: "Details", url: ":toolsetSlug" },
      old: { title: "Old", url: "old", legacyRedirect: true },
    },
  },
  docs: { title: "Docs", url: "https://example.com", external: true },
} satisfies Record<string, RouteEntry>;
const ORG_ROUTE_STRUCTURE = {
  home: { title: "Home", url: "" },
  billing: { title: "Billing", url: "billing" },
};
`;
  const routes = parseCatalogue(src);
  const byKey = Object.fromEntries(routes.map((r) => [r.key, r]));
  assert.equal(byKey["login"]?.path, "/login");
  assert.equal(byKey["login"]?.unauthenticated, true);
  assert.equal(byKey["home"]?.path, "/:orgSlug/projects/:projectSlug");
  assert.equal(byKey["mcp"]?.path, "/:orgSlug/projects/:projectSlug/mcp");
  assert.equal(
    byKey["mcp.add"]?.path,
    "/:orgSlug/projects/:projectSlug/mcp/add",
  );
  assert.equal(
    byKey["mcp.add.remote"]?.path,
    "/:orgSlug/projects/:projectSlug/mcp/add/remote",
  );
  assert.equal(
    byKey["mcp.details"]?.path,
    "/:orgSlug/projects/:projectSlug/mcp/:toolsetSlug",
  );
  assert.equal(byKey["mcp.old"]?.legacyRedirect, true);
  assert.equal(byKey["docs"]?.external, true);
  assert.equal(byKey["org.billing"]?.path, "/:orgSlug/billing");
  assert.equal(byKey["org.billing"]?.title, "Billing");
  assert.equal(byKey["org.home"]?.path, "/:orgSlug");

  const visitable = staticRoutes(routes)
    .map((r) => r.key)
    .sort();
  assert.deepEqual(
    visitable,
    [
      "home",
      "mcp",
      "mcp.add",
      "mcp.add.remote",
      "org.billing",
      "org.home",
    ].sort(),
  );
  assert.equal(
    resolvePath(byKey["mcp"]!, "acme", "p1"),
    "/acme/projects/p1/mcp",
  );
});

test("the real routes.tsx yields the routes the dashboard is known to have", () => {
  const routes = loadCatalogue();
  const paths = new Set(routes.map((r) => r.path));
  for (const expected of [
    "/login",
    "/:orgSlug/projects/:projectSlug",
    "/:orgSlug/projects/:projectSlug/mcp",
    "/:orgSlug/projects/:projectSlug/mcp/add/remote",
    "/:orgSlug/projects/:projectSlug/environments",
    "/:orgSlug/billing",
    "/:orgSlug/team",
  ]) {
    assert.ok(paths.has(expected), `missing ${expected}`);
  }
  const visitable = staticRoutes(routes);
  // routes.tsx had 181 url entries and 36 parameterised ones when this was
  // written. The exact number drifts with the product; the floor catches a
  // parser that silently stops early.
  assert.ok(visitable.length >= 60, `only ${visitable.length} static routes`);
  for (const r of visitable) {
    assert.ok(!r.path.includes(":") || r.path.startsWith("/:orgSlug"), r.path);
    assert.ok(!r.unauthenticated && !r.external && !r.legacyRedirect, r.key);
  }
});
