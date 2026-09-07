import { describe, expect, it } from "vitest";
import { ALL_POLICY_MESSAGE_TYPES } from "./policy-form";
import {
  decodeKindScope,
  describePolicyScope,
  detectionScopesForCategoryEdit,
  effectivePolicyScopeKinds,
  effectiveScopeKinds,
  encodeKindScope,
  kindScopeForMessageTypes,
  narrowScopeToKinds,
  replaceCategoryDetectionScope,
} from "./policy-scope";

const PROMPT_INJECTION_EXEMPT =
  'kind == "assistant_message" || (kind == "tool_request" && tool_calls.exists(t, t.name.matchRegex("^(Read|Grep|Glob|LS|NotebookRead|ExitPlanMode|TodoWrite|AskUserQuestion|ToolSearch|WebSearch)$")) && !tool_calls.exists(t, !t.name.matchRegex("^(Read|Grep|Glob|LS|NotebookRead|ExitPlanMode|TodoWrite|AskUserQuestion|ToolSearch|WebSearch)$")))';

describe("policy scope codec", () => {
  it("replaces one category scope without dropping siblings", () => {
    expect(
      replaceCategoryDetectionScope(
        [
          { category: "secrets", scopeInclude: 'kind == "user_message"' },
          { category: "pii", scopeInclude: 'kind == "tool_request"' },
        ],
        { category: "secrets", scopeInclude: 'kind == "tool_response"' },
      ),
    ).toEqual([
      { category: "pii", scopeInclude: 'kind == "tool_request"' },
      { category: "secrets", scopeInclude: 'kind == "tool_response"' },
    ]);
  });

  it("round-trips canonical kind scopes", () => {
    const encoded = encodeKindScope(["tool_request", "tool_response"]);

    expect(decodeKindScope(encoded)).toEqual(["tool_request", "tool_response"]);
  });

  it("sorts and deduplicates input", () => {
    expect(
      encodeKindScope(["tool_response", "user_message", "tool_response"]),
    ).toBe('kind in ["tool_response","user_message"]');
  });

  it("rejects empty input", () => {
    expect(() => encodeKindScope([])).toThrow(
      "detection scope message types must not be empty",
    );
  });

  it("decodes non-canonical kind lists", () => {
    expect(decodeKindScope('kind in ["user_message","tool_request"]')).toEqual([
      "user_message",
      "tool_request",
    ]);
    expect(decodeKindScope('kind in ["tool_request","tool_request"]')).toEqual([
      "tool_request",
    ]);
    expect(decodeKindScope('kind in [ "tool_request" ]')).toEqual([
      "tool_request",
    ]);
  });

  it("decodes parenthesised and unspaced OR scopes", () => {
    expect(
      decodeKindScope('(kind == "user_message")||(kind == "tool_request")'),
    ).toEqual(["user_message", "tool_request"]);
  });

  it("rejects kind lists that are not message kinds", () => {
    expect(decodeKindScope('kind in ["user_message","nonsense"]')).toBeNull();
    expect(decodeKindScope("kind in []")).toBeNull();
    expect(decodeKindScope('kind in ["user_message"')).toBeNull();
  });

  it("keeps prompt attachments out of the message-type decoding", () => {
    // prompt_attachment is a real scan surface but not a policy message type,
    // so a list carrying it cannot round-trip through the message-type picker.
    expect(
      decodeKindScope('kind in ["tool_request","prompt_attachment"]'),
    ).toBeNull();
    expect(
      effectiveScopeKinds({
        scopeInclude: 'kind in ["tool_request","prompt_attachment"]',
      }),
    ).toEqual({ kinds: new Set(["tool_request"]), custom: false });
  });

  it("decodes surface-only OR scopes from the policy editor", () => {
    expect(
      decodeKindScope('kind == "user_message" || kind == "tool_response"'),
    ).toEqual(["user_message", "tool_response"]);
  });

  it("represents an empty picker as an empty effective scope", () => {
    expect(effectiveScopeKinds(kindScopeForMessageTypes([]))).toEqual({
      kinds: new Set(),
      custom: false,
    });
  });

  it("decodes a single assistant message exemption", () => {
    expect(
      effectiveScopeKinds({ scopeExempt: 'kind == "assistant_message"' }),
    ).toEqual({
      kinds: new Set(["user_message", "tool_request", "tool_response"]),
      custom: false,
    });
  });

  it("marks non-decodable registry expressions as custom", () => {
    expect(
      effectiveScopeKinds({ scopeExempt: PROMPT_INJECTION_EXEMPT }),
    ).toEqual({
      kinds: new Set(ALL_POLICY_MESSAGE_TYPES),
      custom: true,
    });
  });
});

describe("effectivePolicyScopeKinds", () => {
  const categoryDefinitions = [
    {
      key: "secrets",
      recommendedScopeApplicable: true,
      recommendedScopeExempt: 'kind == "assistant_message"',
    },
    {
      key: "shadow_mcp",
      recommendedScopeApplicable: true,
      recommendedScopeInclude: 'kind == "tool_request"',
    },
    {
      key: "account_identity",
      recommendedScopeApplicable: false,
    },
  ];

  it("unions enabled category scopes", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets", "shadow_mcp"],
        categoryDefinitions,
      }).kinds,
    ).toEqual(new Set(["user_message", "tool_request", "tool_response"]));
  });

  it("uses overrides before recommendations and applies the legacy filter", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets"],
        detectionScopes: [
          {
            category: "secrets",
            scopeInclude: encodeKindScope([
              "assistant_message",
              "tool_response",
            ]),
          },
        ],
        categoryDefinitions,
        messageTypes: ["tool_request", "tool_response"],
      }),
    ).toEqual({
      kinds: new Set(["tool_response"]),
      additionalKinds: new Set(),
      custom: false,
    });
  });

  it("intersects category scopes with policy-level CEL", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets"],
        categoryDefinitions,
        scopeInclude: encodeKindScope(["user_message", "tool_response"]),
        scopeExempt: 'kind == "user_message"',
      }),
    ).toEqual({
      kinds: new Set(["tool_response"]),
      additionalKinds: new Set(),
      custom: false,
    });
  });

  it("uses unrestricted category scopes when no definition is applied", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["account_identity"],
      }).kinds,
    ).toEqual(new Set(ALL_POLICY_MESSAGE_TYPES));
  });

  it("includes prompt attachments when the legacy filter is unrestricted", () => {
    expect(
      effectivePolicyScopeKinds({ categories: ["secrets"] }).additionalKinds,
    ).toEqual(new Set(["prompt_attachment"]));
  });

  it("preserves prompt attachments as a display-only legacy kind", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets"],
        messageTypes: ["prompt_attachment"],
      }),
    ).toEqual({
      kinds: new Set(),
      additionalKinds: new Set(["prompt_attachment"]),
      custom: false,
    });
  });

  it("applies policy-level prompt attachment scopes to regular kinds", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets"],
        scopeInclude: 'kind == "prompt_attachment"',
      }),
    ).toEqual({
      kinds: new Set(),
      additionalKinds: new Set(["prompt_attachment"]),
      custom: false,
    });

    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets"],
        scopeInclude: 'kind in ["prompt_attachment","tool_request"]',
      }),
    ).toEqual({
      kinds: new Set(["tool_request"]),
      additionalKinds: new Set(["prompt_attachment"]),
      custom: false,
    });
  });

  it("applies canonical category scopes to prompt attachments", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets"],
        detectionScopes: [
          {
            category: "secrets",
            scopeInclude: encodeKindScope(["tool_request"]),
          },
        ],
        messageTypes: ["prompt_attachment"],
      }).additionalKinds,
    ).toEqual(new Set());
  });

  it("skips categories without message scopes", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["account_identity"],
        categoryDefinitions,
      }).kinds,
    ).toEqual(new Set());
  });

  it("preserves the custom marker from contributing scopes", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["secrets"],
        detectionScopes: [
          { category: "secrets", scopeExempt: PROMPT_INJECTION_EXEMPT },
        ],
        categoryDefinitions,
      }).custom,
    ).toBe(true);
  });
});

describe("narrowScopeToKinds", () => {
  it("replaces a decodable scope with the picked kinds", () => {
    expect(
      narrowScopeToKinds({ scopeExempt: 'kind == "assistant_message"' }, [
        "tool_request",
        "assistant_message",
      ]),
    ).toEqual({
      scopeInclude: 'kind in ["assistant_message","tool_request"]',
    });
  });

  it("carries a custom exemption the kind list cannot express", () => {
    expect(
      narrowScopeToKinds({ scopeExempt: PROMPT_INJECTION_EXEMPT }, [
        "tool_request",
      ]),
    ).toEqual({
      scopeInclude: 'kind in ["tool_request"]',
      scopeExempt: PROMPT_INJECTION_EXEMPT,
    });
  });

  it("intersects a custom include instead of dropping it", () => {
    expect(
      narrowScopeToKinds(
        { scopeInclude: 'tool_calls.exists(t, t.name == "x")' },
        ["tool_request"],
      ),
    ).toEqual({
      scopeInclude:
        '(tool_calls.exists(t, t.name == "x")) && kind in ["tool_request"]',
    });
  });

  it("encodes an empty pick as an empty scope", () => {
    expect(
      effectiveScopeKinds(
        narrowScopeToKinds({ scopeExempt: PROMPT_INJECTION_EXEMPT }, []),
      ),
    ).toEqual({ kinds: new Set(), custom: false });
  });
});

describe("detectionScopesForCategoryEdit", () => {
  const categoryDefinitions = [
    {
      key: "secrets",
      recommendedScopeApplicable: true,
      recommendedScopeExempt: 'kind == "assistant_message"',
    },
    {
      key: "prompt_injection",
      recommendedScopeApplicable: true,
      recommendedScopeExempt: PROMPT_INJECTION_EXEMPT,
    },
    { key: "account_identity", recommendedScopeApplicable: false },
  ];

  it("writes only the edited category when nothing legacy is at stake", () => {
    expect(
      detectionScopesForCategoryEdit({
        category: "secrets",
        kinds: ["tool_request", "tool_response"],
        policyCategories: ["secrets", "pii"],
        categoryDefinitions,
      }),
    ).toEqual([
      {
        category: "secrets",
        scopeInclude: 'kind in ["tool_request","tool_response"]',
      },
    ]);
  });

  it("keeps the recommended predicate the kind list cannot express", () => {
    expect(
      detectionScopesForCategoryEdit({
        category: "prompt_injection",
        kinds: ["tool_response"],
        policyCategories: ["prompt_injection"],
        categoryDefinitions,
      }),
    ).toEqual([
      {
        category: "prompt_injection",
        scopeInclude: 'kind in ["tool_response"]',
        scopeExempt: PROMPT_INJECTION_EXEMPT,
      },
    ]);
  });

  it("pins the legacy narrowing onto the policy's other categories", () => {
    // message_types is cleared alongside this write, so pii would otherwise
    // silently widen from tool traffic to every surface.
    expect(
      detectionScopesForCategoryEdit({
        category: "secrets",
        kinds: ["user_message"],
        policyCategories: ["secrets", "pii", "account_identity"],
        categoryDefinitions,
        messageTypes: ["tool_request", "tool_response"],
      }),
    ).toEqual([
      {
        category: "pii",
        scopeInclude: 'kind in ["tool_request","tool_response"]',
      },
      { category: "secrets", scopeInclude: 'kind in ["user_message"]' },
    ]);
  });

  it("pins the legacy narrowing through an existing category scope", () => {
    expect(
      detectionScopesForCategoryEdit({
        category: "secrets",
        kinds: ["user_message"],
        policyCategories: ["secrets", "prompt_injection"],
        detectionScopes: [
          {
            category: "prompt_injection",
            scopeInclude: 'kind in ["tool_request","user_message"]',
            scopeExempt: PROMPT_INJECTION_EXEMPT,
          },
        ],
        categoryDefinitions,
        messageTypes: ["tool_request", "tool_response"],
      }),
    ).toEqual([
      {
        category: "prompt_injection",
        scopeInclude: 'kind in ["tool_request"]',
        scopeExempt: PROMPT_INJECTION_EXEMPT,
      },
      { category: "secrets", scopeInclude: 'kind in ["user_message"]' },
    ]);
  });

  it.each(["secrets", "prompt_injection"])(
    "round-trips a hydrated %s selection through a scope write",
    (category) => {
      // What the wizard shows must survive being written back: legacy
      // message_types is cleared on write, so the category scope has to carry
      // both the legacy narrowing and the recommendation.
      const messageTypes = ["tool_request", "tool_response"];
      const hydrated = effectivePolicyScopeKinds({
        categories: [category],
        categoryDefinitions,
        messageTypes,
      });

      const detectionScopes = detectionScopesForCategoryEdit({
        category,
        kinds: [...hydrated.kinds],
        policyCategories: [category],
        categoryDefinitions,
        messageTypes,
      });

      expect(
        effectivePolicyScopeKinds({
          categories: [category],
          detectionScopes,
          categoryDefinitions,
        }),
      ).toEqual(hydrated);
    },
  );

  it("preserves scopes for categories the policy no longer covers", () => {
    expect(
      detectionScopesForCategoryEdit({
        category: "secrets",
        kinds: ["user_message"],
        policyCategories: ["secrets"],
        detectionScopes: [
          { category: "custom", scopeInclude: 'kind in ["tool_request"]' },
        ],
        categoryDefinitions,
      }),
    ).toEqual([
      { category: "custom", scopeInclude: 'kind in ["tool_request"]' },
      { category: "secrets", scopeInclude: 'kind in ["user_message"]' },
    ]);
  });
});

describe("describePolicyScope", () => {
  const scope = (
    kinds: string[],
    { custom = false, attachments = false } = {},
  ) =>
    describePolicyScope({
      kinds: new Set(kinds as never),
      additionalKinds: new Set(attachments ? ["prompt_attachment"] : []),
      custom,
    });

  it("summarises whole-surface and tool-call scopes", () => {
    expect(scope(ALL_POLICY_MESSAGE_TYPES).summary).toBe("All types");
    expect(scope(["tool_request", "tool_response"]).summary).toBe("Tool Calls");
  });

  it("never presents a custom CEL scope as a kind list", () => {
    const described = scope(ALL_POLICY_MESSAGE_TYPES, { custom: true });

    expect(described.summary).toBe("Custom scope");
    expect(described.tooltip).toContain("At most");
  });

  it("reports an empty scope as such", () => {
    expect(scope([])).toEqual({
      summary: "Nothing in scope",
      tooltip: "No message types in scope",
    });
    expect(scope([], { custom: true }).summary).toBe("Custom scope");
  });

  it("lists prompt attachments alongside message types", () => {
    expect(scope(["tool_request"], { attachments: true }).summary).toBe(
      "Tool Requests, Prompt Attachments",
    );
  });
});
