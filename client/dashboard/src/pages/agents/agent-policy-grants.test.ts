import { describe, expect, it } from "vitest";
import {
  AGENT_POLICY_SCOPES,
  agentPolicyFingerprint,
  agentPolicyDraftFromGrants,
  agentPolicyGrantsFromDraft,
  agentPolicyResourceKind,
  agentPolicyViewFromGrants,
  diffAgentPolicyGrants,
  isAgentPolicyGrantRepresentable,
  isAgentPolicyNarrowable,
} from "./agent-policy-grants";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";

function storedGrant(
  id: string,
  scope: string,
  selector: AgentPolicyGrant["selector"],
): AgentPolicyGrant {
  return {
    id,
    scope,
    effect: "allow",
    selector,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  };
}

describe("agent policy scope catalog", () => {
  // Mirrors the safeRuntimeScope() entries of runtimeScopeDefinitions in
  // server/internal/agents/runtimepolicy/scopes.go. A scope the server does not
  // consider agent-runtime-safe is rejected with 400, so drift here is a bug in
  // one direction and a missing capability in the other.
  it("offers exactly the agent-runtime-safe scopes", () => {
    expect(AGENT_POLICY_SCOPES.map((scope) => scope.slug).sort()).toEqual([
      "environment:read",
      "environment:write",
      "mcp:connect",
      "mcp:read",
      "mcp:write",
      "project:read",
      "project:write",
      "risk_policy:evaluate",
      "skill:read",
      "skill:write",
    ]);
  });

  it("never offers a scope outside the safe set", () => {
    for (const scope of AGENT_POLICY_SCOPES) {
      expect(scope.slug).not.toMatch(/blocked_/);
      expect(scope.slug.split(":")[0]).not.toBe("org");
      expect(scope.slug.split(":")[0]).not.toBe("chat");
      expect(scope.slug.split(":")[0]).not.toBe("agent");
      expect(scope.slug).not.toBe("risk_policy:bypass");
      expect(scope.slug).not.toBe("risk_policy:block");
    }
  });

  it("fixes the resource kind from the scope family", () => {
    for (const scope of AGENT_POLICY_SCOPES) {
      expect(agentPolicyResourceKind(scope.slug)).toBe(scope.resourceType);
    }
  });

  it("withholds a resource choice where the picker cannot name the right kind", () => {
    expect(isAgentPolicyNarrowable("environment")).toBe(false);
    expect(isAgentPolicyNarrowable("risk_policy")).toBe(false);
    expect(isAgentPolicyNarrowable("mcp")).toBe(true);
    expect(isAgentPolicyNarrowable("skill")).toBe(true);
  });
});

describe("agent policy draft to grants", () => {
  it("adds nothing for an empty draft", () => {
    expect(agentPolicyGrantsFromDraft({})).toEqual([]);
  });

  it("sends one wildcard grant per unrestricted permission", () => {
    expect(
      agentPolicyGrantsFromDraft({ "mcp:connect": null, "skill:read": null }),
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
      {
        effect: "allow",
        scope: "skill:read",
        selector: { resourceKind: "skill", resourceId: "*" },
      },
    ]);
  });

  it("sends one grant per narrowed resource and keeps the MCP dimensions", () => {
    expect(
      agentPolicyGrantsFromDraft({
        "mcp:connect": [
          {
            resourceKind: "mcp",
            resourceId: "server_one",
            tool: "search",
            disposition: "read_only",
            projectId: "project_one",
          },
          { resourceKind: "mcp", resourceId: "server_two" },
        ],
      }),
    ).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: "server_one",
          projectId: "project_one",
          disposition: "read_only",
          tool: "search",
        },
      },
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server_two" },
      },
    ]);
  });

  // authz.ValidateSelector derives resource_kind from the scope and rejects a
  // mismatch, and rejects any extra key outside allowedSelectorKeys.
  it("forces the scope's own resource kind and drops keys it does not allow", () => {
    expect(
      agentPolicyGrantsFromDraft({
        "skill:read": [
          {
            resourceKind: "mcp",
            resourceId: "project_one",
            tool: "search",
            disposition: "destructive",
            projectId: "project_two",
          },
        ],
      }),
    ).toEqual([
      {
        effect: "allow",
        scope: "skill:read",
        selector: { resourceKind: "skill", resourceId: "project_one" },
      },
    ]);
  });

  it("omits a wildcard dimension rather than sending it", () => {
    expect(
      agentPolicyGrantsFromDraft({
        "mcp:read": [
          { resourceKind: "mcp", resourceId: "*", projectId: "*", tool: "*" },
        ],
      })[0]?.selector,
    ).toEqual({ resourceKind: "mcp", resourceId: "*" });
  });

  it("collapses duplicates the server would reject", () => {
    expect(
      agentPolicyGrantsFromDraft({
        "mcp:read": [
          { resourceKind: "mcp", resourceId: "server_one" },
          { resourceKind: "mcp", resourceId: "server_one" },
        ],
      }),
    ).toHaveLength(1);
  });
});

describe("stored ceiling to draft", () => {
  it("reads a wildcard grant as unrestricted", () => {
    expect(
      agentPolicyDraftFromGrants([
        storedGrant("grant_one", "mcp:connect", {
          resourceKind: "mcp",
          resourceId: "*",
        }),
      ]),
    ).toEqual({ "mcp:connect": null });
  });

  it("collects narrowed grants for one scope into one permission", () => {
    expect(
      agentPolicyDraftFromGrants([
        storedGrant("grant_one", "mcp:connect", {
          resourceKind: "mcp",
          resourceId: "server_one",
        }),
        storedGrant("grant_two", "mcp:connect", {
          resourceKind: "mcp",
          resourceId: "server_two",
          tool: "search",
        }),
      ]),
    ).toEqual({
      "mcp:connect": [
        { resourceKind: "mcp", resourceId: "server_one" },
        { resourceKind: "mcp", resourceId: "server_two", tool: "search" },
      ],
    });
  });

  it("keeps an unrestricted permission unrestricted when a narrower grant sits beside it", () => {
    expect(
      agentPolicyDraftFromGrants([
        storedGrant("grant_one", "mcp:read", {
          resourceKind: "mcp",
          resourceId: "*",
        }),
        storedGrant("grant_two", "mcp:read", {
          resourceKind: "mcp",
          resourceId: "server_one",
        }),
      ]),
    ).toEqual({ "mcp:read": null });
  });

  it("round-trips a stored ceiling without proposing any change", () => {
    const stored = [
      storedGrant("grant_one", "mcp:connect", {
        resourceKind: "mcp",
        resourceId: "server_one",
        tool: "search",
      }),
      storedGrant("grant_two", "project:read", {
        resourceKind: "project",
        resourceId: "*",
      }),
    ];
    const diff = diffAgentPolicyGrants(
      stored,
      agentPolicyGrantsFromDraft(agentPolicyDraftFromGrants(stored)),
    );
    expect(diff).toEqual({ create: [], remove: [] });
  });
});

describe("policy diff", () => {
  it("treats a narrowed permission as a removal and an addition", () => {
    const stored = [
      storedGrant("grant_one", "mcp:connect", {
        resourceKind: "mcp",
        resourceId: "*",
      }),
    ];
    const diff = diffAgentPolicyGrants(
      stored,
      agentPolicyGrantsFromDraft({
        "mcp:connect": [{ resourceKind: "mcp", resourceId: "server_one" }],
      }),
    );
    expect(diff.remove.map((grant) => grant.id)).toEqual(["grant_one"]);
    expect(diff.create).toEqual([
      {
        effect: "allow",
        scope: "mcp:connect",
        selector: { resourceKind: "mcp", resourceId: "server_one" },
      },
    ]);
  });

  it("removes everything when the ceiling is emptied", () => {
    const stored = [
      storedGrant("grant_one", "mcp:connect", {
        resourceKind: "mcp",
        resourceId: "*",
      }),
      storedGrant("grant_two", "skill:read", {
        resourceKind: "skill",
        resourceId: "*",
      }),
    ];
    const diff = diffAgentPolicyGrants(stored, agentPolicyGrantsFromDraft({}));
    expect(diff.create).toEqual([]);
    expect(diff.remove.map((grant) => grant.id).sort()).toEqual([
      "grant_one",
      "grant_two",
    ]);
  });
});

// The server accepts server_url and server_identity on risk policy selectors
// (allowedSelectorKeys in server/internal/authz/selector.go), but the editor
// has no control for either. Rewriting such a grant through the draft would
// drop the constraint, so it is preserved verbatim and its scope is locked.
describe("stored constraints the editor cannot show", () => {
  const riskGrant = (
    id: string,
    selector: AgentPolicyGrant["selector"],
  ): AgentPolicyGrant => storedGrant(id, "risk_policy:evaluate", selector);

  it.each([
    [
      "server_url",
      {
        resourceKind: "risk_policy" as const,
        resourceId: "*",
        serverUrl: "https://mcp.example.test",
      },
    ],
    [
      "server_identity",
      {
        resourceKind: "risk_policy" as const,
        resourceId: "*",
        serverIdentity: "identity_one",
      },
    ],
    [
      "both dimensions",
      {
        resourceKind: "risk_policy" as const,
        resourceId: "*",
        serverUrl: "https://mcp.example.test",
        serverIdentity: "identity_one",
      },
    ],
  ])(
    "keeps a risk policy grant constrained by %s exactly as stored",
    (_, selector) => {
      const stored = [riskGrant("grant_risk", selector)];
      const view = agentPolicyViewFromGrants(stored);
      expect(isAgentPolicyGrantRepresentable(stored[0]!)).toBe(false);
      expect(view.preserved).toEqual(stored);
      expect(view.editable).toEqual([]);
      expect(view.preservedScopes).toEqual(["risk_policy:evaluate"]);
      // Absent from the draft, so no edit can rewrite or widen it.
      expect(view.draft).toEqual({});
    },
  );

  it("leaves a preserved grant untouched while an unrelated permission is added", () => {
    const preserved = riskGrant("grant_risk", {
      resourceKind: "risk_policy",
      resourceId: "*",
      serverUrl: "https://mcp.example.test",
      serverIdentity: "identity_one",
    });
    const view = agentPolicyViewFromGrants([preserved]);
    const diff = diffAgentPolicyGrants(
      view.editable,
      agentPolicyGrantsFromDraft({ ...view.draft, "mcp:read": null }),
    );
    expect(diff.remove).toEqual([]);
    expect(diff.create).toEqual([
      {
        effect: "allow",
        scope: "mcp:read",
        selector: { resourceKind: "mcp", resourceId: "*" },
      },
    ]);
  });

  it("locks every grant of a scope when only one of them is unrepresentable", () => {
    const constrained = riskGrant("grant_risk", {
      resourceKind: "risk_policy",
      resourceId: "*",
      serverUrl: "https://mcp.example.test",
    });
    const plain = riskGrant("grant_plain", {
      resourceKind: "risk_policy",
      resourceId: "*",
    });
    const view = agentPolicyViewFromGrants([constrained, plain]);
    expect(view.preserved).toEqual([constrained, plain]);
    expect(view.editable).toEqual([]);
    expect(
      diffAgentPolicyGrants(
        view.editable,
        agentPolicyGrantsFromDraft(view.draft),
      ),
    ).toEqual({ create: [], remove: [] });
  });

  it("does not treat a wildcard-only risk policy grant as unrepresentable", () => {
    const plain = riskGrant("grant_plain", {
      resourceKind: "risk_policy",
      resourceId: "*",
    });
    expect(isAgentPolicyGrantRepresentable(plain)).toBe(true);
    expect(agentPolicyViewFromGrants([plain]).draft).toEqual({
      "risk_policy:evaluate": null,
    });
  });

  it("locks a grant whose resource kind does not match its scope", () => {
    const mismatched = storedGrant("grant_bad", "skill:read", {
      resourceKind: "mcp",
      resourceId: "server_one",
    });
    const view = agentPolicyViewFromGrants([mismatched]);
    expect(view.preserved).toEqual([mismatched]);
    expect(view.draft).toEqual({});
  });

  it("still reads a wildcard extra dimension as unconstrained", () => {
    const wildcarded = storedGrant("grant_wild", "mcp:read", {
      resourceKind: "mcp",
      resourceId: "*",
      projectId: "*",
    });
    expect(isAgentPolicyGrantRepresentable(wildcarded)).toBe(true);
    expect(agentPolicyDraftFromGrants([wildcarded])).toEqual({
      "mcp:read": null,
    });
  });
});

// The server stores whatever string it was handed. An empty tool or project id
// still constrains the grant, so truthiness must not decide whether it survives
// a trip through the editor.
describe("empty-string dimensions", () => {
  it.each([
    ["tool", { tool: "" }],
    ["projectId", { projectId: "" }],
    ["both", { tool: "", projectId: "" }],
  ])("round-trips a grant constrained by an empty %s", (_, extra) => {
    const stored = [
      storedGrant("grant_empty", "mcp:connect", {
        resourceKind: "mcp",
        resourceId: "server_one",
        ...extra,
      }),
    ];
    const view = agentPolicyViewFromGrants(stored);
    expect(view.editable).toEqual(stored);
    expect(
      diffAgentPolicyGrants(
        view.editable,
        agentPolicyGrantsFromDraft(view.draft),
      ),
    ).toEqual({ create: [], remove: [] });
  });

  it("does not read an empty dimension as unrestricted", () => {
    expect(
      agentPolicyDraftFromGrants([
        storedGrant("grant_empty", "mcp:connect", {
          resourceKind: "mcp",
          resourceId: "*",
          tool: "",
        }),
      ]),
    ).toEqual({
      "mcp:connect": [{ resourceKind: "mcp", resourceId: "*", tool: "" }],
    });
  });

  it("leaves it alone while an unrelated permission is added", () => {
    const stored = [
      storedGrant("grant_empty", "mcp:connect", {
        resourceKind: "mcp",
        resourceId: "server_one",
        tool: "",
      }),
    ];
    const view = agentPolicyViewFromGrants(stored);
    const diff = diffAgentPolicyGrants(
      view.editable,
      agentPolicyGrantsFromDraft({ ...view.draft, "skill:read": null }),
    );
    expect(diff.remove).toEqual([]);
    expect(diff.create).toEqual([
      {
        effect: "allow",
        scope: "skill:read",
        selector: { resourceKind: "skill", resourceId: "*" },
      },
    ]);
  });
});

describe("ceiling fingerprint", () => {
  const base = [
    storedGrant("grant_one", "mcp:connect", {
      resourceKind: "mcp",
      resourceId: "server_one",
    }),
  ];

  it("ignores the order grants arrive in", () => {
    const second = storedGrant("grant_two", "skill:read", {
      resourceKind: "skill",
      resourceId: "*",
    });
    expect(agentPolicyFingerprint([...base, second])).toBe(
      agentPolicyFingerprint([second, ...base]),
    );
  });

  it.each([
    [
      "an added grant",
      [
        ...base,
        storedGrant("grant_two", "skill:read", {
          resourceKind: "skill",
          resourceId: "*",
        }),
      ],
    ],
    ["a removed grant", []],
    [
      "a grant rewritten in place",
      [
        storedGrant("grant_one", "mcp:connect", {
          resourceKind: "mcp",
          resourceId: "server_two",
        }),
      ],
    ],
    [
      "a change to a preserved grant",
      [
        ...base,
        storedGrant("grant_risk", "risk_policy:evaluate", {
          resourceKind: "risk_policy",
          resourceId: "*",
          serverUrl: "https://mcp.example.test",
        }),
      ],
    ],
  ])("changes for %s", (_, next) => {
    expect(agentPolicyFingerprint(next)).not.toBe(agentPolicyFingerprint(base));
  });
});
