import { describe, expect, it } from "vitest";
import { ALL_POLICY_MESSAGE_TYPES } from "./policy-data";
import {
  acceptsDetectionScope,
  decodeKindScope,
  policyScopeUpdateForCategoryEdit,
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

  it("decodes the empty kind list as nothing in scope", () => {
    expect(decodeKindScope("kind in []")).toEqual([]);
    expect(effectiveScopeKinds({ scopeInclude: "kind in []" })).toEqual({
      kinds: new Set(),
      custom: false,
    });
  });

  it("stops reporting custom once nothing is in scope", () => {
    // Include and exempt can only narrow, so an undecodable exemption cannot
    // put anything back — reporting it as custom would hide an empty scope.
    expect(
      effectiveScopeKinds({
        scopeInclude: "kind in []",
        scopeExempt: PROMPT_INJECTION_EXEMPT,
      }),
    ).toEqual({ kinds: new Set(), custom: false });
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
      sessionScopedOnly: false,
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
      sessionScopedOnly: false,
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
      sessionScopedOnly: false,
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
      sessionScopedOnly: false,
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
      sessionScopedOnly: false,
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

  it("reports a policy whose categories are all session-scoped", () => {
    expect(
      effectivePolicyScopeKinds({
        categories: ["account_identity"],
        categoryDefinitions,
      }).sessionScopedOnly,
    ).toBe(true);
    expect(
      effectivePolicyScopeKinds({
        categories: ["account_identity", "secrets"],
        categoryDefinitions,
      }).sessionScopedOnly,
    ).toBe(false);
    expect(
      effectivePolicyScopeKinds({ categories: [], categoryDefinitions })
        .sessionScopedOnly,
    ).toBe(false);
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

describe("policyScopeUpdateForCategoryEdit", () => {
  const categoryDefinitions = [
    {
      key: "secrets",
      recommendedScopeApplicable: true,
      recommendedScopeExempt: 'kind == "assistant_message"',
    },
    {
      key: "pii",
      recommendedScopeApplicable: true,
      recommendedScopeExempt: 'kind == "assistant_message"',
    },
    {
      key: "prompt_injection",
      recommendedScopeApplicable: true,
      recommendedScopeExempt: PROMPT_INJECTION_EXEMPT,
    },
    { key: "custom", recommendedScopeApplicable: true },
    { key: "account_identity", recommendedScopeApplicable: false },
  ];

  it("writes only the edited category when nothing legacy is at stake", () => {
    expect(
      policyScopeUpdateForCategoryEdit({
        category: "secrets",
        kinds: ["tool_request", "tool_response"],
        policyCategories: ["secrets", "pii"],
        categoryDefinitions,
      }),
    ).toEqual({
      detectionScopes: [
        {
          category: "secrets",
          scopeInclude: 'kind in ["tool_request","tool_response"]',
        },
      ],
      messageTypes: [],
    });
  });

  it("keeps the recommended predicate the kind list cannot express", () => {
    expect(
      policyScopeUpdateForCategoryEdit({
        category: "prompt_injection",
        kinds: ["tool_response"],
        policyCategories: ["prompt_injection"],
        categoryDefinitions,
      }).detectionScopes,
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
      policyScopeUpdateForCategoryEdit({
        category: "secrets",
        kinds: ["user_message"],
        policyCategories: ["secrets", "pii", "account_identity"],
        categoryDefinitions,
        messageTypes: ["tool_request", "tool_response"],
      }),
    ).toEqual({
      detectionScopes: [
        {
          category: "pii",
          scopeInclude: 'kind in ["tool_request","tool_response"]',
        },
        { category: "secrets", scopeInclude: 'kind in ["user_message"]' },
      ],
      messageTypes: [],
    });
  });

  it("pins the legacy narrowing through an existing category scope", () => {
    expect(
      policyScopeUpdateForCategoryEdit({
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
      }).detectionScopes,
    ).toEqual([
      {
        category: "prompt_injection",
        scopeInclude: 'kind in ["tool_request"]',
        scopeExempt: PROMPT_INJECTION_EXEMPT,
      },
      { category: "secrets", scopeInclude: 'kind in ["user_message"]' },
    ]);
  });

  it("never puts a scope the API rejects on the wire", () => {
    // Session-scoped categories reject message scoping outright and a scope
    // for one fails the whole update. `custom` is not one of them: it has no
    // recommendation, but the API accepts a specified scope for it.
    const update = policyScopeUpdateForCategoryEdit({
      category: "secrets",
      kinds: ["user_message"],
      policyCategories: ["secrets", "custom", "account_identity"],
      categoryDefinitions,
      messageTypes: ["tool_request"],
    });

    expect(update.detectionScopes.map((scope) => scope.category)).toEqual([
      "custom",
      "secrets",
    ]);
    expect(acceptsDetectionScope(categoryDefinitions[3])).toBe(true);
    expect(acceptsDetectionScope(categoryDefinitions[4])).toBe(false);
  });

  it("clears the legacy list once every covered category can be pinned", () => {
    // `custom` accepts a scope now, so its share of the legacy narrowing is
    // pinned onto it and the legacy list goes away entirely.
    const update = policyScopeUpdateForCategoryEdit({
      category: "secrets",
      kinds: ["user_message"],
      policyCategories: ["secrets", "custom"],
      categoryDefinitions,
      messageTypes: ["tool_request"],
    });

    expect(update.messageTypes).toEqual([]);
    expect(update.detectionScopes).toContainEqual({
      category: "custom",
      scopeInclude: 'kind in ["tool_request"]',
    });
  });

  it("keeps the legacy list for a category it has no definition for", () => {
    // An unrecognized category cannot be pinned, so clearing message_types
    // would widen it; the list survives, gaining only the edited kinds.
    expect(
      policyScopeUpdateForCategoryEdit({
        category: "secrets",
        kinds: ["user_message"],
        policyCategories: ["secrets", "not_a_category"],
        categoryDefinitions,
        messageTypes: ["tool_request"],
      }).messageTypes,
    ).toEqual(["tool_request", "user_message"]);
  });

  it("preserves scopes for categories the policy no longer covers", () => {
    expect(
      policyScopeUpdateForCategoryEdit({
        category: "secrets",
        kinds: ["user_message"],
        policyCategories: ["secrets"],
        detectionScopes: [
          { category: "custom", scopeInclude: 'kind in ["tool_request"]' },
        ],
        categoryDefinitions,
      }).detectionScopes,
    ).toEqual([
      { category: "custom", scopeInclude: 'kind in ["tool_request"]' },
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

      const { detectionScopes } = policyScopeUpdateForCategoryEdit({
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

  it.each([
    ["a recommended", undefined, PROMPT_INJECTION_EXEMPT],
    [
      "an overridden",
      [
        {
          category: "prompt_injection",
          scopeInclude: 'kind in ["tool_request"]',
          scopeExempt: PROMPT_INJECTION_EXEMPT,
        },
      ],
      PROMPT_INJECTION_EXEMPT,
    ],
  ])(
    "survives disable and re-enable with %s custom exemption",
    (_label, detectionScopes, exempt) => {
      // Unchecking every message type must not be a destructive rewrite: the
      // read-only-tool exemption has to come back when a type is re-checked.
      const disabled = policyScopeUpdateForCategoryEdit({
        category: "prompt_injection",
        kinds: [],
        policyCategories: ["prompt_injection"],
        detectionScopes: detectionScopes as never,
        categoryDefinitions,
      }).detectionScopes;

      expect(disabled).toEqual([
        {
          category: "prompt_injection",
          scopeInclude: "kind in []",
          scopeExempt: exempt,
        },
      ]);
      expect(
        effectivePolicyScopeKinds({
          categories: ["prompt_injection"],
          detectionScopes: disabled,
          categoryDefinitions,
        }),
      ).toEqual({
        kinds: new Set(),
        additionalKinds: new Set(),
        custom: false,
        sessionScopedOnly: false,
      });

      expect(
        policyScopeUpdateForCategoryEdit({
          category: "prompt_injection",
          kinds: ["tool_request"],
          policyCategories: ["prompt_injection"],
          detectionScopes: disabled,
          categoryDefinitions,
        }).detectionScopes,
      ).toEqual([
        {
          category: "prompt_injection",
          scopeInclude: 'kind in ["tool_request"]',
          scopeExempt: exempt,
        },
      ]);
    },
  );

  it("keeps a custom include across an empty selection", () => {
    const customInclude = 'tool_calls.exists(t, t.name == "Bash")';
    const disabled = policyScopeUpdateForCategoryEdit({
      category: "secrets",
      kinds: [],
      policyCategories: ["secrets"],
      detectionScopes: [{ category: "secrets", scopeInclude: customInclude }],
      categoryDefinitions,
    }).detectionScopes;

    expect(disabled).toEqual([
      {
        category: "secrets",
        scopeInclude: `(${customInclude}) && kind in []`,
      },
    ]);
    expect(
      policyScopeUpdateForCategoryEdit({
        category: "secrets",
        kinds: ["tool_request"],
        policyCategories: ["secrets"],
        detectionScopes: disabled,
        categoryDefinitions,
      }).detectionScopes,
    ).toEqual([
      {
        category: "secrets",
        scopeInclude: `(${customInclude}) && kind in ["tool_request"]`,
      },
    ]);
  });
});
