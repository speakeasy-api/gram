import { describe, expect, it } from "vitest";

import { isPortablePath, resolvePortablePath } from "./portable-path";

const ORG = { slug: "acme" };

const loc = (pathname: string, search = "", hash = "") => ({
  pathname,
  search,
  hash,
});

describe("isPortablePath", () => {
  it("matches the bare prefix and nested paths", () => {
    expect(isPortablePath("/@self")).toBe(true);
    expect(isPortablePath("/@self/webhooks")).toBe(true);
  });

  it("rejects ordinary and lookalike paths", () => {
    expect(isPortablePath("/")).toBe(false);
    expect(isPortablePath("/acme/projects/default")).toBe(false);
    expect(isPortablePath("/@selfish/settings")).toBe(false);
    expect(isPortablePath("/@other/settings")).toBe(false);
    // The retired "/~" prefix is an ordinary path now.
    expect(isPortablePath("/~/toolsets")).toBe(false);
  });
});

describe("resolvePortablePath", () => {
  it("returns undefined for non-portable paths", () => {
    expect(resolvePortablePath(loc("/acme/toolsets"), ORG)).toBeUndefined();
  });

  it("expands into the org, keeping the rest of the path", () => {
    expect(resolvePortablePath(loc("/@self/settings/members"), ORG)).toBe(
      "/acme/settings/members",
    );
  });

  it("expands the bare prefix to the org home", () => {
    expect(resolvePortablePath(loc("/@self"), ORG)).toBe("/acme");
  });

  // Docs link project pages as /@self/projects/default/<page>; they must land
  // on the default project, never the viewer's last-visited one.
  it("keeps an explicit project", () => {
    expect(
      resolvePortablePath(loc("/@self/projects/default/toolsets"), ORG),
    ).toBe("/acme/projects/default/toolsets");
  });

  it("keeps the destination's query and hash", () => {
    expect(
      resolvePortablePath(loc("/@self/billing", "?tab=usage", "#top"), ORG),
    ).toBe("/acme/billing?tab=usage#top");
  });
});
