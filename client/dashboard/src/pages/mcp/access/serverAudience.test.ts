import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { describe, expect, it } from "vitest";
import { blockingRules, effectiveReach } from "./serverAudience";

function entry(
  overrides: Partial<ResourceAudienceEntry> & { principalUrn: string },
): ResourceAudienceEntry {
  return {
    kind: "role",
    displayName: "GTM",
    level: "use",
    appliesTo: "all_resources",
    ...overrides,
  } as ResourceAudienceEntry;
}

const gtmGrant = entry({ principalUrn: "role:organization:gtm", level: "use" });
const adminBlock = entry({
  principalUrn: "role:global:admin",
  displayName: "Admin",
  level: "blocked",
  appliesTo: "resource",
});

describe("blockingRules", () => {
  // The shape that made a role of eleven show four people underneath it: the
  // seven missing were administrators too, and a block on that role outranks
  // the grant that put them in the list.
  it("names the block that cancelled every capability", () => {
    const reaching = [gtmGrant, adminBlock];

    expect(effectiveReach(reaching)).toBeNull();
    expect(blockingRules(reaching).map((rule) => rule.displayName)).toEqual([
      "Admin",
    ]);
  });

  it("says nothing about someone the rules still reach", () => {
    expect(blockingRules([gtmGrant])).toEqual([]);
  });

  it("says nothing about someone no rule names", () => {
    expect(blockingRules([])).toEqual([]);
  });

  // A block narrowed to some tools leaves the rest reachable, so it is not
  // what took someone off the server and must not be named as if it were.
  it("ignores a block that only trims tools", () => {
    const reaching = [
      gtmGrant,
      entry({
        principalUrn: "role:global:admin",
        displayName: "Admin",
        level: "blocked",
        appliesTo: "resource",
        dispositions: ["destructive"],
      }),
    ];

    expect(effectiveReach(reaching)).not.toBeNull();
    expect(blockingRules(reaching)).toEqual([]);
  });
});

describe("blockingRules names only the blocks that took something away", () => {
  // The mcp:blocked_* scopes are independent. A block on a capability nobody
  // was granted changed nothing, and naming it sends an administrator to a
  // role that is not the reason.
  it("ignores a block on a capability the grants never opened", () => {
    const reaching = [
      gtmGrant,
      adminBlock,
      entry({
        principalUrn: "role:organization:ops",
        displayName: "Ops",
        level: "blocked_manage",
        appliesTo: "resource",
      }),
    ];

    expect(blockingRules(reaching).map((rule) => rule.displayName)).toEqual([
      "Admin",
    ]);
  });
});
