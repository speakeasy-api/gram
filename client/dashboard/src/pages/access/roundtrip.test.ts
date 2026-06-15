import { describe, it, expect } from "vitest";
import type { Role } from "@gram/client/models/components/role.js";
import type { RoleGrant } from "./types";
import {
  applyRemoveRule,
  diffGrants,
  grantsFromRole,
  sdkGrantsFromForm,
} from "./roleGrantTransform";

function role(grants: Role["grants"]): Role {
  return {
    id: "role_test",
    principalUrn: "role:organization:role_test",
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

describe("diffGrants", () => {
  it("treats two risk-policy selectors differing only in serverUrl as distinct", () => {
    // serverUrl is the differentiating dimension for risk_policy scopes.
    // selectorKey must include it or the diff collapses both grants to the
    // same identity and the change is silently dropped.
    const before = [
      {
        scope: "risk_policy:evaluate" as const,
        effect: "allow" as const,
        selectors: [
          {
            resourceKind: "risk_policy" as const,
            resourceId: "*",
            serverUrl: "https://a.example.com",
          },
        ],
      },
    ];
    const after = [
      {
        scope: "risk_policy:evaluate" as const,
        effect: "allow" as const,
        selectors: [
          {
            resourceKind: "risk_policy" as const,
            resourceId: "*",
            serverUrl: "https://b.example.com",
          },
        ],
      },
    ];

    const { addGrants, removeGrants } = diffGrants(before, after);

    expect(removeGrants).toHaveLength(1);
    expect(addGrants).toHaveLength(1);
  });
});

describe("applyRemoveRule", () => {
  it("unchecks the scope when an unrestricted allow is removed, dropping orphaned denies", () => {
    const grant: RoleGrant = {
      scope: "mcp:connect",
      rules: [
        { id: "a", effect: "allow", selectors: null },
        {
          id: "d",
          effect: "deny",
          selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
        },
      ],
    };

    expect(applyRemoveRule(grant, 0)).toBeNull();
  });

  it("falls back to unrestricted when the only narrower allow is removed", () => {
    const grant: RoleGrant = {
      scope: "mcp:connect",
      rules: [
        {
          id: "a",
          effect: "allow",
          selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
        },
      ],
    };

    const next = applyRemoveRule(grant, 0);
    expect(next).not.toBeNull();
    expect(next!.rules).toHaveLength(1);
    expect(next!.rules[0]!.effect).toBe("allow");
    expect(next!.rules[0]!.selectors).toBeNull();
  });

  it("filters a deny without touching surviving allows", () => {
    const grant: RoleGrant = {
      scope: "mcp:connect",
      rules: [
        { id: "a", effect: "allow", selectors: null },
        {
          id: "d",
          effect: "deny",
          selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
        },
      ],
    };

    const next = applyRemoveRule(grant, 1);
    expect(next).not.toBeNull();
    expect(next!.rules).toHaveLength(1);
    expect(next!.rules[0]!.effect).toBe("allow");
  });
});
