import { describe, expect, it } from "vitest";

import { resolveEntityLink } from "./assistantEntityLinkResolve";

describe("resolveEntityLink", () => {
  it("resolves gram:skill/<id> to the project skill detail page", () => {
    expect(
      resolveEntityLink(
        "gram:skill/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
        "acme",
        "default",
      ),
    ).toEqual({
      href: "/acme/projects/default/skills/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
      target: "_blank",
      rel: "noopener noreferrer",
    });
  });

  it("URL-encodes skill ids", () => {
    expect(
      resolveEntityLink("gram:skill/id with spaces", "acme", "proj"),
    ).toEqual({
      href: "/acme/projects/proj/skills/id%20with%20spaces",
      target: "_blank",
      rel: "noopener noreferrer",
    });
  });

  it("returns unresolvable when the project slug is missing", () => {
    expect(resolveEntityLink("gram:skill/abc", "acme", undefined)).toEqual({
      href: null,
    });
  });

  it("returns unresolvable for an empty skill id", () => {
    expect(resolveEntityLink("gram:skill/", "acme", "proj")).toEqual({
      href: null,
    });
  });
});
