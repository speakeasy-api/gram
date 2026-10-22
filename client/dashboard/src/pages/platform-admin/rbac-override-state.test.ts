import { beforeEach, expect, it } from "vitest";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import {
  loadCatalog,
  loadState,
  saveCatalog,
  saveState,
} from "./rbac-override-state";

beforeEach(() => localStorage.clear());

it("reconciles catalog and saved choices in the header without mounting the editor", () => {
  saveState({
    enabled: true,
    scopes: {
      "org:read": { enabled: false, resources: null },
      "saved:read": { enabled: true, resources: ["resource-a"] },
    },
  });
  const scope = {
    slug: "new:read",
    resourceType: "new",
    description: "New scope",
    visibility: "user_visible",
  };
  saveCatalog([
    scope,
    { ...scope, slug: "new:blocked_read", visibility: "internal" },
  ]);
  expect(loadCatalog().map((scope) => scope.slug)).toEqual([
    "org:read",
    "new:read",
  ]);
  expect(loadState().scopes["org:read"]?.enabled).toBe(false);
  expect(getRBACScopeOverrideHeader(true)?.split(",").sort()).toEqual([
    "new:read",
    "saved:read=resource-a",
  ]);
});

it("distinguishes a disabled override from an active override with no grants", () => {
  const scopes = { "org:read": { enabled: false, resources: null } };
  saveState({ enabled: true, scopes });
  expect(getRBACScopeOverrideHeader(true)).toBe("rbac-override:none");
  expect(getRBACScopeOverrideHeader(false)).toBeNull();
  saveState({ enabled: false, scopes });
  expect(getRBACScopeOverrideHeader(true)).toBeNull();
});
