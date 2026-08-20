import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { describe, expect, it } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  isBlockingShadowMCPPolicy,
  isShadowMCPBlockConfiguration,
  shadowMCPAllowedURLsForMutation,
  shadowMCPBlockedURLsForMutation,
  shadowMCPDecisionConflicts,
  shadowMCPSelectionBaselineForUpdate,
  shadowMCPSelectionIsDirty,
  shadowMCPSelectionIsInitialized,
} from "./policy-shadow-mcp-setup";

const blockingShadowMCPPolicy = {
  action: "block",
  enabled: true,
  sources: ["shadow_mcp"],
} satisfies Pick<RiskPolicy, "action" | "enabled" | "sources">;

describe("isBlockingShadowMCPPolicy", () => {
  it("recognizes an enabled blocking Shadow MCP policy", () => {
    expect(
      isBlockingShadowMCPPolicy(true, ["shadow_mcp", "secrets"], "block"),
    ).toBe(true);
  });

  it.each([
    [false, ["shadow_mcp"], "block"],
    [true, ["shadow_mcp"], "flag"],
    [true, ["secrets"], "block"],
  ] as const)(
    "rejects non-target policy state %#",
    (enabled, sources, action) => {
      expect(isBlockingShadowMCPPolicy(enabled, sources, action)).toBe(false);
    },
  );
});

describe("isShadowMCPBlockConfiguration", () => {
  it("recognizes a disabled blocking Shadow MCP policy configuration", () => {
    expect(isShadowMCPBlockConfiguration(["shadow_mcp"], "block")).toBe(true);
  });
});

describe("shadowMCPAllowedURLsForMutation", () => {
  it("returns sorted selected URLs for a target blocking Shadow MCP policy", () => {
    expect(
      shadowMCPAllowedURLsForMutation({
        action: "block",
        selectedCategories: new Set(["shadow_mcp"]),
        selectedURLs: new Set([
          "https://linear.example.com/sse",
          "https://github.example.com/mcp",
        ]),
        originalPolicy: null,
      }),
    ).toEqual([
      "https://github.example.com/mcp",
      "https://linear.example.com/sse",
    ]);
  });

  it("clears grants when an existing blocking policy changes to flag", () => {
    expect(
      shadowMCPAllowedURLsForMutation({
        action: "flag",
        selectedCategories: new Set(["shadow_mcp"]),
        selectedURLs: new Set(["https://github.example.com/mcp"]),
        originalPolicy: blockingShadowMCPPolicy,
      }),
    ).toEqual([]);
  });

  it("clears grants when a disabled blocking policy changes to flag", () => {
    expect(
      shadowMCPAllowedURLsForMutation({
        action: "flag",
        selectedCategories: new Set(["shadow_mcp"]),
        selectedURLs: new Set(["https://github.example.com/mcp"]),
        originalPolicy: { ...blockingShadowMCPPolicy, enabled: false },
      }),
    ).toEqual([]);
  });

  it("omits grants for an unrelated policy create", () => {
    expect(
      shadowMCPAllowedURLsForMutation({
        action: "flag",
        selectedCategories: new Set(["secrets"]),
        selectedURLs: new Set(),
        originalPolicy: null,
      }),
    ).toBeUndefined();
  });
});

describe("shadowMCPSelectionIsDirty", () => {
  it("marks a changed selection dirty for a blocking Shadow MCP draft", () => {
    expect(
      shadowMCPSelectionIsDirty(
        true,
        new Set(["https://github.example.com/mcp"]),
        new Set(),
      ),
    ).toBe(true);
  });

  it("ignores hidden selection changes for a non-target draft", () => {
    expect(
      shadowMCPSelectionIsDirty(
        false,
        new Set(["https://github.example.com/mcp"]),
        new Set(),
      ),
    ).toBe(false);
  });
});

describe("shadowMCPSelectionIsInitialized", () => {
  it("blocks a target draft until the current editor identity is initialized", () => {
    expect(shadowMCPSelectionIsInitialized(true, null, "policy-1")).toBe(false);
    expect(shadowMCPSelectionIsInitialized(true, "policy-2", "policy-1")).toBe(
      false,
    );
    expect(shadowMCPSelectionIsInitialized(true, "policy-1", "policy-1")).toBe(
      true,
    );
  });

  it("does not gate a non-target draft", () => {
    expect(shadowMCPSelectionIsInitialized(false, null, "policy-1")).toBe(true);
  });
});

describe("shadowMCPSelectionBaselineForUpdate", () => {
  it("returns the explicitly submitted URL set", () => {
    expect(
      shadowMCPSelectionBaselineForUpdate({
        shadowMcpAllowedUrls: ["https://github.example.com/mcp"],
      }),
    ).toEqual(new Set(["https://github.example.com/mcp"]));
  });

  it("returns an empty baseline for an explicit clear", () => {
    expect(
      shadowMCPSelectionBaselineForUpdate({ shadowMcpAllowedUrls: [] }),
    ).toEqual(new Set());
  });

  it("does not invent a baseline when the field was omitted", () => {
    expect(shadowMCPSelectionBaselineForUpdate({})).toBeUndefined();
  });
});

describe("shadowMCPAllowedURLsForMutation with allow_all disposition", () => {
  it("never sends allowed URLs for an allow_all target", () => {
    expect(
      shadowMCPAllowedURLsForMutation({
        action: "block",
        selectedCategories: new Set(["shadow_mcp"]),
        selectedURLs: new Set(["https://github.example.com/mcp"]),
        originalPolicy: null,
        disposition: "allow_all",
      }),
    ).toBeUndefined();
  });
});

describe("shadowMCPBlockedURLsForMutation", () => {
  it("returns sorted selected URLs for an allow_all blocking Shadow MCP target", () => {
    expect(
      shadowMCPBlockedURLsForMutation({
        action: "block",
        selectedCategories: new Set(["shadow_mcp"]),
        selectedURLs: new Set([
          "https://sketchy.example.com/mcp",
          "https://bad.example.com/sse",
        ]),
        disposition: "allow_all",
      }),
    ).toEqual([
      "https://bad.example.com/sse",
      "https://sketchy.example.com/mcp",
    ]);
  });

  it("omits blocked URLs under the block_all disposition", () => {
    expect(
      shadowMCPBlockedURLsForMutation({
        action: "block",
        selectedCategories: new Set(["shadow_mcp"]),
        selectedURLs: new Set(["https://sketchy.example.com/mcp"]),
        disposition: "block_all",
      }),
    ).toBeUndefined();
  });

  it("omits blocked URLs when the target is not a blocking Shadow MCP policy", () => {
    expect(
      shadowMCPBlockedURLsForMutation({
        action: "flag",
        selectedCategories: new Set(["shadow_mcp"]),
        selectedURLs: new Set(["https://sketchy.example.com/mcp"]),
        disposition: "allow_all",
      }),
    ).toBeUndefined();
  });
});

describe("shadowMCPSelectionBaselineForUpdate with blocked URLs", () => {
  it("returns the explicitly submitted blocked URL set", () => {
    expect(
      shadowMCPSelectionBaselineForUpdate({
        shadowMcpBlockedUrls: ["https://sketchy.example.com/mcp"],
      }),
    ).toEqual(new Set(["https://sketchy.example.com/mcp"]));
  });

  it("returns an empty baseline for an explicit blocked-list clear", () => {
    expect(
      shadowMCPSelectionBaselineForUpdate({ shadowMcpBlockedUrls: [] }),
    ).toEqual(new Set());
  });
});

describe("shadowMCPDecisionConflicts", () => {
  const server = (
    url: string,
    status: "approved" | "denied" | "requested" | "superseded",
  ): ShadowMCPInventoryServer =>
    ({
      canonicalServerUrl: url,
      serverName: url.replace("https://", ""),
      approvalRequest: { id: `req-${url}`, status, requesterCount: 0 },
    }) as ShadowMCPInventoryServer;

  const approvedURL = "https://approved.example.com/mcp";
  const deniedURL = "https://denied.example.com/mcp";
  const servers = [
    server(approvedURL, "approved"),
    server(deniedURL, "denied"),
  ];

  it("flags unchecking an approved server from a block_all allow list", () => {
    const conflicts = shadowMCPDecisionConflicts({
      servers,
      originalURLs: new Set([approvedURL]),
      selectedURLs: new Set(),
      disposition: "block_all",
    });
    expect(conflicts).toEqual([
      {
        canonicalServerUrl: approvedURL,
        serverName: "approved.example.com/mcp",
        decision: "approved",
      },
    ]);
  });

  it("flags allow-listing a denied server", () => {
    const conflicts = shadowMCPDecisionConflicts({
      servers,
      originalURLs: new Set([approvedURL]),
      selectedURLs: new Set([approvedURL, deniedURL]),
      disposition: "block_all",
    });
    expect(conflicts.map((c) => c.decision)).toEqual(["denied"]);
  });

  it("inverts directions on an allow_all block list", () => {
    // Block-listing the approved server and unblocking the denied one both
    // contradict standing decisions.
    const conflicts = shadowMCPDecisionConflicts({
      servers,
      originalURLs: new Set([deniedURL]),
      selectedURLs: new Set([approvedURL]),
      disposition: "allow_all",
    });
    expect(conflicts.map((c) => c.canonicalServerUrl)).toEqual([
      approvedURL,
      deniedURL,
    ]);
  });

  it("ignores unchanged rows, undecided reviews, and superseded decisions", () => {
    const conflicts = shadowMCPDecisionConflicts({
      servers: [
        ...servers,
        server("https://pending.example.com/mcp", "requested"),
        server("https://superseded.example.com/mcp", "superseded"),
      ],
      originalURLs: new Set([approvedURL]),
      selectedURLs: new Set([
        approvedURL,
        "https://pending.example.com/mcp",
        "https://superseded.example.com/mcp",
      ]),
      disposition: "block_all",
    });
    expect(conflicts).toEqual([]);
  });

  it("returns nothing while the baseline is unknown", () => {
    expect(
      shadowMCPDecisionConflicts({
        servers,
        originalURLs: null,
        selectedURLs: new Set(),
        disposition: "block_all",
      }),
    ).toEqual([]);
  });
});
