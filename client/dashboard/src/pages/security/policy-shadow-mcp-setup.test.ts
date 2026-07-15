import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { describe, expect, it, vi } from "vitest";
import {
  idempotencyKeyForFingerprint,
  isBlockingShadowMCPPolicy,
  shadowMCPAllowedURLsForMutation,
  type SubmissionKeyCache,
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

describe("idempotencyKeyForFingerprint", () => {
  it("reuses the key for the same request fingerprint", () => {
    const cache: SubmissionKeyCache = { current: null };
    const createKey = vi.fn(() => "key-1");

    expect(idempotencyKeyForFingerprint(cache, "body-a", createKey)).toBe(
      "key-1",
    );
    expect(idempotencyKeyForFingerprint(cache, "body-a", createKey)).toBe(
      "key-1",
    );
    expect(createKey).toHaveBeenCalledOnce();
  });

  it("creates a new key when the request fingerprint changes", () => {
    const cache: SubmissionKeyCache = { current: null };
    const createKey = vi
      .fn<() => string>()
      .mockReturnValueOnce("key-1")
      .mockReturnValueOnce("key-2");

    expect(idempotencyKeyForFingerprint(cache, "body-a", createKey)).toBe(
      "key-1",
    );
    expect(idempotencyKeyForFingerprint(cache, "body-b", createKey)).toBe(
      "key-2",
    );
  });
});
