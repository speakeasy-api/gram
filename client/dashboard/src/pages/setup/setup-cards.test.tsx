import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { SETUP_CARDS, setupTaskKeyForSlug, setupTaskSlug } from "./setup-cards";

function serverCatalogKeys(): string[] {
  const server = readFileSync(
    resolve(
      import.meta.dirname,
      "../../../../../server/internal/organizations/setup_tasks.go",
    ),
    "utf8",
  );
  const catalog = server.match(
    /var setupTaskCatalog = \[\]setupTaskDefinition\{([\s\S]*?)^\}/m,
  )?.[1];
  expect(catalog).toBeDefined();
  return Array.from(catalog!.matchAll(/\bKey:\s*"([^"]+)"/g), (m) => m[1]!);
}

describe("setup cards", () => {
  it("has exactly one card per server catalog task", () => {
    const serverKeys = serverCatalogKeys();
    expect(serverKeys.length).toBeGreaterThan(0);
    expect(Object.keys(SETUP_CARDS).sort()).toEqual([...serverKeys].sort());
  });

  it("maps every card to a distinct slug and back", () => {
    const slugs = Object.keys(SETUP_CARDS).map(setupTaskSlug);
    expect(new Set(slugs).size).toBe(slugs.length);
    for (const key of Object.keys(SETUP_CARDS)) {
      expect(setupTaskKeyForSlug(setupTaskSlug(key))).toBe(key);
    }
    expect(setupTaskSlug("identity-provider")).toBe("idp");
  });

  it("still resolves a task key used as a slug, and nothing else", () => {
    expect(setupTaskKeyForSlug("identity-provider")).toBe("identity-provider");
    expect(setupTaskKeyForSlug("nope")).toBeUndefined();
    expect(setupTaskKeyForSlug("toString")).toBeUndefined();
  });
});
