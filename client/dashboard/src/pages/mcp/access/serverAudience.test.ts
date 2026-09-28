import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
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

describe("principal precedence", () => {
  const hana = "user:u1";
  const ownRule = (overrides: Partial<ResourceAudienceEntry> = {}) =>
    entry({
      principalUrn: hana,
      kind: "user",
      displayName: "Hana Sato",
      appliesTo: "resource",
      ...overrides,
    });

  it("lets a person's own rule here outrank a role's block", () => {
    const reaching = [gtmGrant, adminBlock, ownRule()];

    expect(effectiveReach(reaching, [], hana)?.toolsLabel).toBe("All tools");
    expect(blockingRules(reaching, hana)).toEqual([]);
  });

  it("keeps a narrowed own rule's tools past a role's block", () => {
    const reaching = [gtmGrant, adminBlock, ownRule({ tools: ["search"] })];

    expect(effectiveReach(reaching, [], hana)?.toolsLabel).toBe("Search");
  });

  it("keeps the person's own block", () => {
    const reaching = [
      ownRule(),
      ownRule({ level: "blocked", appliesTo: "resource" }),
    ];

    expect(effectiveReach(reaching, [], hana)).toBeNull();
  });

  it("lets a narrowed own view rule outrank a role's view block", () => {
    const viewBlock = entry({
      principalUrn: "role:global:admin",
      displayName: "Admin",
      level: "blocked_view",
      appliesTo: "resource",
    });
    const reaching = [
      entry({ principalUrn: "role:organization:gtm", level: "view" }),
      viewBlock,
      ownRule({ level: "view", tools: ["search"] }),
    ];

    expect(effectiveReach(reaching, [], hana)?.capabilities).toContain("view");
  });

  it("does not let a rule covering every server outrank a block", () => {
    const reaching = [adminBlock, ownRule({ appliesTo: "all_resources" })];

    expect(effectiveReach(reaching, [], hana)).toBeNull();
  });
});

describe("effective tool counts with direct overrides", () => {
  const user = "user:1";
  const catalog: ToolSelectionTool[] = [
    { name: "search", annotations: ["read_only"] },
    { name: "delete", annotations: ["destructive"] },
  ];
  const direct = entry({
    principalUrn: user,
    kind: "user",
    appliesTo: "resource",
    tools: ["search"],
  });

  it("counts a tool restored past a narrowed role block", () => {
    const reach = effectiveReach(
      [gtmGrant, { ...adminBlock, tools: ["search"] }, direct],
      catalog,
      user,
    );
    expect(reach?.toolsLabel).toBe("2 tools");
    expect(reach?.reachableTools).toEqual(["search", "delete"]);
  });

  it("counts only directly granted tools past a whole-server role block", () => {
    const reach = effectiveReach([gtmGrant, adminBlock, direct], catalog, user);
    expect(reach?.toolsLabel).toBe("1 tool");
    expect(reach?.reachableTools).toEqual(["search"]);
  });

  it("keeps the user's own narrowed block effective", () => {
    const reach = effectiveReach(
      [
        gtmGrant,
        { ...adminBlock, tools: ["search"] },
        direct,
        { ...direct, level: "blocked" },
      ],
      catalog,
      user,
    );
    expect(reach?.toolsLabel).toBe("1 tool");
    expect(reach?.reachableTools).toEqual(["delete"]);
  });

  it("restores only the tools covered by a direct disposition grant", () => {
    const reach = effectiveReach(
      [
        gtmGrant,
        adminBlock,
        { ...direct, tools: [], dispositions: ["read_only"] },
      ],
      catalog,
      user,
    );
    expect(reach?.reachableTools).toEqual(["search"]);
  });
});
