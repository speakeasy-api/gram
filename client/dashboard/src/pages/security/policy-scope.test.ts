import { describe, expect, it } from "vitest";
import { ALL_POLICY_MESSAGE_TYPES } from "./policy-form";
import {
  decodeKindScope,
  effectivePolicyScopeKinds,
  effectiveScopeKinds,
  encodeKindScope,
} from "./policy-scope";

const PROMPT_INJECTION_EXEMPT =
  'kind == "assistant_message" || (kind == "tool_request" && tool_calls.exists(t, t.name.matchRegex("^(Read|Grep|Glob|LS|NotebookRead|ExitPlanMode|TodoWrite|AskUserQuestion|ToolSearch|WebSearch)$")) && !tool_calls.exists(t, !t.name.matchRegex("^(Read|Grep|Glob|LS|NotebookRead|ExitPlanMode|TodoWrite|AskUserQuestion|ToolSearch|WebSearch)$")))';

describe("policy scope codec", () => {
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
    ).toEqual({ kinds: new Set(["tool_response"]), custom: false });
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
