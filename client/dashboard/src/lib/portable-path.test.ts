import { describe, expect, it } from "vitest";

import { isPortablePath, resolvePortablePath } from "./portable-path";

const ORG = {
  slug: "acme",
  projects: [{ slug: "proj-a" }, { slug: "proj-b" }],
};

const loc = (pathname: string, search = "", hash = "") => ({
  pathname,
  search,
  hash,
});

describe("isPortablePath", () => {
  it("matches the bare prefix and nested paths", () => {
    expect(isPortablePath("/~")).toBe(true);
    expect(isPortablePath("/~/toolsets")).toBe(true);
  });

  it("rejects ordinary and lookalike paths", () => {
    expect(isPortablePath("/")).toBe(false);
    expect(isPortablePath("/acme/projects/default")).toBe(false);
    // "~foo" could be a real (if odd) first segment; only the exact "~"
    // segment is the placeholder.
    expect(isPortablePath("/~foo/toolsets")).toBe(false);
    expect(isPortablePath("/@selfish/settings")).toBe(false);
    expect(isPortablePath("/@other/settings")).toBe(false);
  });

  it("matches the org-level /@self prefix", () => {
    expect(isPortablePath("/@self")).toBe(true);
    expect(isPortablePath("/@self/settings")).toBe(true);
  });
});

describe("resolvePortablePath", () => {
  it("returns undefined for non-portable paths", () => {
    expect(resolvePortablePath(loc("/acme/toolsets"), ORG)).toBeUndefined();
  });

  it("expands into the org and first project", () => {
    expect(resolvePortablePath(loc("/~/toolsets"), ORG)).toBe(
      "/acme/projects/proj-a/toolsets",
    );
  });

  it("expands the bare prefix to the project home", () => {
    expect(resolvePortablePath(loc("/~"), ORG)).toBe("/acme/projects/proj-a");
  });

  it("prefers the last-visited project when it still exists", () => {
    expect(resolvePortablePath(loc("/~/toolsets"), ORG, "proj-b")).toBe(
      "/acme/projects/proj-b/toolsets",
    );
  });

  it("ignores a preferred project that is no longer visible", () => {
    expect(resolvePortablePath(loc("/~/toolsets"), ORG, "gone")).toBe(
      "/acme/projects/proj-a/toolsets",
    );
  });

  it("keeps the destination's query and hash", () => {
    expect(
      resolvePortablePath(loc("/~/toolsets", "?tab=all", "#top"), ORG),
    ).toBe("/acme/projects/proj-a/toolsets?tab=all#top");
  });

  it("falls back to the org home when no project is visible", () => {
    const org = { slug: "acme", projects: [] };
    expect(resolvePortablePath(loc("/~/toolsets", "?tab=all"), org)).toBe(
      "/acme?tab=all",
    );
  });

  it("expands /@self into the org, keeping the rest of the path", () => {
    expect(resolvePortablePath(loc("/@self/settings/members"), ORG)).toBe(
      "/acme/settings/members",
    );
  });

  it("expands bare /@self to the org home", () => {
    expect(resolvePortablePath(loc("/@self"), ORG)).toBe("/acme");
  });

  it("keeps /@self query and hash and ignores the preferred project", () => {
    expect(
      resolvePortablePath(
        loc("/@self/billing", "?tab=usage", "#top"),
        ORG,
        "proj-b",
      ),
    ).toBe("/acme/billing?tab=usage#top");
  });

  it("expands /@self for an org with no visible projects", () => {
    const org = { slug: "acme", projects: [] };
    expect(resolvePortablePath(loc("/@self/settings"), org)).toBe(
      "/acme/settings",
    );
  });
});
