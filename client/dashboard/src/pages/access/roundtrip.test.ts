import { describe, it, expect } from "vitest";
import type { Role } from "@gram/client/models/components/role.js";
import { grantsFromRole, sdkGrantsFromForm } from "./roleGrantTransform";

function role(grants: Role["grants"]): Role {
  return {
    id: "role_test",
    name: "Test",
    slug: "org-test",
    description: "",
    isSystem: false,
    grants,
    memberCount: 0,
    createdAt: new Date("2026-06-05T00:00:00Z"),
    updatedAt: new Date("2026-06-05T00:00:00Z"),
  };
}

describe("role grant round-trip (grantsFromRole → sdkGrantsFromForm)", () => {
  it("collapses the synthetic wildcard allow to unrestricted (selectors:undefined)", () => {
    // Server stores {kind:"mcp", id:"*"} for grants saved with nil selectors.
    // The dialog must collapse this back to the unrestricted shape so the chip
    // reads "All servers" instead of "1 server".
    const r = role([
      {
        scope: "mcp:connect",
        effect: "allow",
        selectors: [{ resourceKind: "mcp", resourceId: "*" }],
      },
    ]);

    const sdkGrants = sdkGrantsFromForm(grantsFromRole(r));

    expect(sdkGrants).toEqual([{ scope: "mcp:connect", selectors: undefined }]);
  });

  it("preserves an explicit wildcard deny grant", () => {
    // Backend stores the kind-scoped wildcard {kind:"mcp", id:"*"} for both
    // allow and deny effects. Allow can be collapsed to unrestricted because
    // sdkGrantsFromForm re-emits unrestricted via selectors:undefined.
    // Deny has no such fallback — if we collapse it to selectors:null,
    // sdkGrantsFromForm drops the rule entirely and the deny is lost on save.
    const r = role([
      {
        scope: "mcp:connect",
        effect: "allow",
        selectors: [{ resourceKind: "mcp", resourceId: "*" }],
      },
      {
        scope: "mcp:connect",
        effect: "deny",
        selectors: [{ resourceKind: "mcp", resourceId: "*" }],
      },
    ]);

    const rules = grantsFromRole(r);
    const sdkGrants = sdkGrantsFromForm(rules);

    expect(sdkGrants).toContainEqual(
      expect.objectContaining({
        scope: "mcp:connect",
        effect: "deny",
      }),
    );
  });
});
