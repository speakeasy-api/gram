import { describe, expect, it } from "vitest";
import { ALL_POLICY_MESSAGE_TYPES } from "./policy-form";
import {
  decodeKindScope,
  effectivePolicyScopeKinds,
  effectiveScopeKinds,
  encodeKindScope,
  kindScopeForMessageTypes,
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

  it("rejects non-canonical kind scopes", () => {
    expect(
      decodeKindScope('kind in ["user_message","tool_request"]'),
    ).toBeNull();
    expect(
      decodeKindScope('kind in ["tool_request","tool_request"]'),
    ).toBeNull();
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

  it("uses unrestricted category scopes when recommendations are disabled", () => {
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
