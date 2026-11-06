// @vitest-environment node
import { describe, expect, it } from "vitest";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import {
  agentPolicyChangedScopes,
  agentPolicyFingerprint,
  agentPolicyDraftFromGrants,
  agentPolicyGrantsFromDraft,
  agentPolicyResourceKind,
  agentPolicyViewFromGrants,
  diffAgentPolicyGrants,
  isAgentPolicyEditorScope,
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
  // The scopes an agent may hold come from access.listAgentPolicyScopes;
  // TestService_ListAgentPolicyScopes_OffersExactlyTheRuntimeSafeScopes pins
  // the set on the server. What stays here is the selector shape per family.
  it("fixes the resource kind from the scope family", () => {
    for (const [slug, resourceType] of [
      ["inference:execute", "inference"],
      ["mcp:connect", "mcp"],
      ["mcp:write", "mcp"],
      ["project:read", "project"],
      ["environment:write", "environment"],
      ["skill:read", "skill"],
      ["assistant:write", "assistant"],
      ["risk_policy:evaluate", "risk_policy"],
      ["org:device_agent_sync", "org"],
      ["org:hooks_ingest", "org"],
    ] as const) {
      expect(agentPolicyResourceKind(slug)).toBe(resourceType);
    }
    expect(agentPolicyResourceKind("widget:read")).toBe("*");
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

  it("sends device agent permissions as organization-wide org grants", () => {
    expect(
      agentPolicyGrantsFromDraft({
        "org:device_agent_sync": null,
        "org:hooks_ingest": null,
      }),
    ).toEqual([
      {
        effect: "allow",
        scope: "org:device_agent_sync",
        selector: { resourceKind: "org", resourceId: "*" },
      },
      {
        effect: "allow",
        scope: "org:hooks_ingest",
        selector: { resourceKind: "org", resourceId: "*" },
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
// (allowedSelectorKeys in server/internal/authz/selector.go) and the editor has
// no control for either, so rewriting one through the draft would drop the
// constraint.
describe("MCP server grants", () => {
  it("leaves mcp:connect out of the editor and out of every diff", () => {
    const connect = storedGrant("grant_connect", "mcp:connect", {
      resourceKind: "mcp",
      resourceId: "server_one",
      projectId: "project_one",
    });
    const read = storedGrant("grant_read", "mcp:read", {
      resourceKind: "mcp",
      resourceId: "*",
    });
    const view = agentPolicyViewFromGrants([connect, read]);
    expect(view.editable).toEqual([read]);
    expect(view.preserved).toEqual([]);
    expect(view.draft).toEqual({ "mcp:read": null });
    expect(
      diffAgentPolicyGrants(view.editable, agentPolicyGrantsFromDraft({})),
    ).toEqual({ create: [], remove: [read] });
    expect(isAgentPolicyEditorScope("mcp:connect")).toBe(false);
    expect(isAgentPolicyEditorScope("mcp:read")).toBe(true);
  });
});

describe("stored constraints the editor cannot show", () => {
  it("preserves the whole scope when a grant has an unsupported effect", () => {
    const allow = storedGrant("grant_allow", "mcp:read", {
      resourceKind: "mcp",
      resourceId: "*",
    });
    const future = {
      ...allow,
      id: "grant_future",
      effect: unrecognized("future_effect"),
    };
    const view = agentPolicyViewFromGrants([allow, future]);
    expect(view.editable).toEqual([]);
    expect(view.draft).toEqual({});
    expect(view.preserved).toEqual([allow, future]);
    expect(view.preservedScopes).toEqual(["mcp:read"]);
    expect(
      diffAgentPolicyGrants(
        view.editable,
        agentPolicyGrantsFromDraft({ ...view.draft, "project:read": null }),
      ).remove,
    ).toEqual([]);
    expect(agentPolicyFingerprint([future])).not.toEqual(
      agentPolicyFingerprint([{ ...future, effect: "allow" }]),
    );
    expect(
      diffAgentPolicyGrants([future], [{ ...future, effect: "allow" }]),
    ).toEqual({
      create: [{ ...future, effect: "allow" }],
      remove: [future],
    });
  });

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

  it("keeps a hook events grant narrowed to one project exactly as stored", () => {
    const stored = [
      storedGrant("grant_hooks", "org:hooks_ingest", {
        resourceKind: "org",
        resourceId: "*",
        projectId: "project_one",
      }),
    ];
    const view = agentPolicyViewFromGrants(stored);
    expect(isAgentPolicyGrantRepresentable(stored[0]!)).toBe(false);
    expect(view.preserved).toEqual(stored);
    expect(view.preservedScopes).toEqual(["org:hooks_ingest"]);
    expect(view.draft).toEqual({});
  });

  it("edits an organization-wide device agent grant and writes it back unchanged", () => {
    const stored = [
      storedGrant("grant_sync", "org:device_agent_sync", {
        resourceKind: "org",
        resourceId: "*",
      }),
    ];
    const view = agentPolicyViewFromGrants(stored);
    expect(view.preservedScopes).toEqual([]);
    expect(view.draft).toEqual({ "org:device_agent_sync": null });
    expect(
      diffAgentPolicyGrants(stored, agentPolicyGrantsFromDraft(view.draft)),
    ).toEqual({ create: [], remove: [] });
  });

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

  it("edits a grant naming one assistant and writes it back unchanged", () => {
    const own = storedGrant("grant_own", "assistant:write", {
      resourceKind: "assistant",
      resourceId: "assistant_one",
    });
    const view = agentPolicyViewFromGrants([own]);
    expect(view.preserved).toEqual([]);
    expect(view.draft).toEqual({
      "assistant:write": [
        { resourceKind: "assistant", resourceId: "assistant_one" },
      ],
    });
    expect(
      diffAgentPolicyGrants(
        view.editable,
        agentPolicyGrantsFromDraft(view.draft),
      ),
    ).toEqual({ create: [], remove: [] });
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

// An empty tool or project id still constrains the grant, so truthiness must
// not decide whether it survives a trip through the editor.
describe("empty-string dimensions", () => {
  it("round-trips a grant constrained by an empty projectId", () => {
    const stored = [
      storedGrant("grant_empty", "mcp:read", {
        resourceKind: "mcp",
        resourceId: "server_one",
        projectId: "",
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

  // The editor shows no tool choice for the MCP scopes it offers, so even an
  // empty tool is a constraint it cannot show, and the scope is kept whole.
  it.each([
    ["tool", { tool: "" }],
    ["both", { tool: "", projectId: "" }],
  ])("keeps a grant constrained by an empty %s as stored", (_, extra) => {
    const stored = [
      storedGrant("grant_empty", "mcp:read", {
        resourceKind: "mcp",
        resourceId: "server_one",
        ...extra,
      }),
    ];
    const view = agentPolicyViewFromGrants(stored);
    expect(view.editable).toEqual([]);
    expect(view.preserved).toEqual(stored);
    expect(view.preservedScopes).toEqual(["mcp:read"]);
  });

  it("does not read an empty dimension as unrestricted", () => {
    expect(
      agentPolicyDraftFromGrants([
        storedGrant("grant_empty", "mcp:read", {
          resourceKind: "mcp",
          resourceId: "*",
          tool: "",
        }),
      ]),
    ).toEqual({
      "mcp:read": [{ resourceKind: "mcp", resourceId: "*", tool: "" }],
    });
  });

  it("leaves it alone while an unrelated permission is added", () => {
    const stored = [
      storedGrant("grant_empty", "mcp:read", {
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

// Sorting the keys defeats the obvious `key=value` join collisions, but two
// adjacent free-form dimensions still collide: `server_identity` is validated
// nowhere and sorts immediately before `server_url`, so an identity ending in
// `&serverUrl=…` keys identically to a grant constraining both.
describe("selector values containing the old delimiters", () => {
  const split = storedGrant("grant_split", "risk_policy:evaluate", {
    resourceKind: "risk_policy",
    resourceId: "*",
    serverIdentity: "x",
    serverUrl: "https://a.test",
  });
  const joined = storedGrant("grant_joined", "risk_policy:evaluate", {
    resourceKind: "risk_policy",
    resourceId: "*",
    serverIdentity: "x&serverUrl=https://a.test",
  });

  it("fingerprints two such grants differently", () => {
    expect(agentPolicyFingerprint([split])).not.toBe(
      agentPolicyFingerprint([joined]),
    );
  });

  it("does not let one stand in for the other in a ceiling of the same size", () => {
    expect(agentPolicyFingerprint([split])).not.toBe(
      agentPolicyFingerprint([{ ...joined, id: split.id }]),
    );
  });

  it("keeps them apart in a diff", () => {
    const diff = diffAgentPolicyGrants(
      [split],
      [{ effect: "allow", scope: joined.scope, selector: joined.selector }],
    );
    expect(diff.remove.map((grant) => grant.id)).toEqual(["grant_split"]);
    expect(diff.create).toHaveLength(1);
  });

  it("keeps them apart in the duplicate check", () => {
    expect(
      agentPolicyGrantsFromDraft({
        "mcp:connect": [
          { resourceKind: "mcp", resourceId: "s", projectId: "a", tool: "b" },
          { resourceKind: "mcp", resourceId: "s", projectId: "a&tool=b" },
        ],
      }),
    ).toHaveLength(2);
  });

  it.each([
    ["an ampersand", "one&two"],
    ["an equals sign", "one=two"],
    ["both", "one&two=three"],
    ["a quote and a backslash", 'one"two\\three'],
    ["an empty string", ""],
  ])("survives a server identity containing %s", (_, serverIdentity) => {
    const grant = storedGrant("grant_odd", "risk_policy:evaluate", {
      resourceKind: "risk_policy",
      resourceId: "*",
      serverIdentity,
    });
    const bare = storedGrant("grant_odd", "risk_policy:evaluate", {
      resourceKind: "risk_policy",
      resourceId: "*",
    });
    expect(agentPolicyFingerprint([grant])).not.toBe(
      agentPolicyFingerprint([bare]),
    );
  });

  it("stays independent of the order the grants arrive in", () => {
    expect(agentPolicyFingerprint([split, joined])).toBe(
      agentPolicyFingerprint([joined, split]),
    );
  });
});

// Loading an agent's ceiling into the editor and saving it untouched must
// write nothing, whatever shape the stored grants take.
describe("round trip through the editor", () => {
  const ceiling = [
    storedGrant("connect", "mcp:connect", {
      resourceKind: "mcp",
      resourceId: "server_one",
      projectId: "project_one",
    }),
    storedGrant("auth", "mcp:auth", {
      resourceKind: "mcp_auth_client",
      resourceId: "*",
    }),
    storedGrant("read_server", "mcp:read", {
      resourceKind: "mcp",
      resourceId: "server_one",
      projectId: "project_one",
    }),
    storedGrant("read_server_two", "mcp:read", {
      resourceKind: "mcp",
      resourceId: "server_two",
    }),
    storedGrant("write_project", "mcp:write", {
      resourceKind: "mcp",
      resourceId: "*",
      projectId: "project_one",
    }),
    storedGrant("project_read", "project:read", {
      resourceKind: "project",
      resourceId: "project_one",
    }),
    storedGrant("skill_read", "skill:read", {
      resourceKind: "skill",
      resourceId: "*",
    }),
    storedGrant("inference", "inference:execute", {
      resourceKind: "inference",
      resourceId: "*",
      projectId: "project_one",
    }),
    storedGrant("assistant", "assistant:read", {
      resourceKind: "assistant",
      resourceId: "assistant_one",
      projectId: "project_one",
    }),
    storedGrant("sync", "org:device_agent_sync", {
      resourceKind: "org",
      resourceId: "org_one",
    }),
  ];

  it("reproduces every editable grant exactly", () => {
    const view = agentPolicyViewFromGrants(ceiling);
    expect(view.preservedScopes).toEqual(["mcp:auth"]);
    expect(view.editable.map((grant) => grant.id)).toEqual([
      "read_server",
      "read_server_two",
      "write_project",
      "project_read",
      "skill_read",
      "inference",
      "assistant",
      "sync",
    ]);
    expect(
      diffAgentPolicyGrants(
        view.editable,
        agentPolicyGrantsFromDraft(view.draft),
      ),
    ).toEqual({ create: [], remove: [] });
    // Rebuilt from what a save would write, the draft names no changed scope.
    expect(
      agentPolicyChangedScopes(
        view.draft,
        agentPolicyDraftFromGrants(
          agentPolicyGrantsFromDraft(view.draft).map((grant, index) =>
            storedGrant(`saved_${index}`, grant.scope, grant.selector),
          ),
        ),
      ),
    ).toEqual(new Set());
  });

  it("locks a scope the picker cannot show in one view", () => {
    const view = agentPolicyViewFromGrants([
      storedGrant("project_wide", "mcp:read", {
        resourceKind: "mcp",
        resourceId: "*",
        projectId: "project_one",
      }),
      storedGrant("named", "mcp:read", {
        resourceKind: "mcp",
        resourceId: "server_two",
      }),
      storedGrant("environment", "environment:read", {
        resourceKind: "environment",
        resourceId: "*",
        projectId: "project_one",
      }),
      storedGrant("risk", "risk_policy:evaluate", {
        resourceKind: "risk_policy",
        resourceId: "policy_one",
      }),
    ]);
    expect(view.editable).toEqual([]);
    expect(view.preservedScopes).toEqual([
      "mcp:read",
      "environment:read",
      "risk_policy:evaluate",
    ]);
  });
});

describe("changed scopes", () => {
  const initial = {
    "mcp:read": [
      {
        resourceKind: "mcp" as const,
        resourceId: "server_one",
        projectId: "p",
      },
    ],
    "project:read": null,
  };

  it("leaves out a scope chosen again as it was", () => {
    expect(
      agentPolicyChangedScopes(initial, {
        ...initial,
        "mcp:read": [
          { resourceKind: "mcp", resourceId: "server_one", projectId: "p" },
        ],
      }),
    ).toEqual(new Set());
  });

  it("names a scope whose resources, presence or completeness changed", () => {
    expect(
      agentPolicyChangedScopes(initial, {
        "mcp:read": [],
        "skill:read": null,
      }),
    ).toEqual(new Set(["mcp:read", "project:read", "skill:read"]));
  });
});
