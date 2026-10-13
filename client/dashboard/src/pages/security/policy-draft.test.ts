import type { SuggestRiskPolicyResult } from "@gram/client/models/components/suggestriskpolicyresult.js";
import { beforeEach, describe, expect, it } from "vitest";
import {
  draftFromPreset,
  draftFromSuggestion,
  draftKind,
  draftMCPScopeValue,
  matchMCPServers,
  readPolicyDraft,
  sessionOnlyReason,
  clearPolicyStartSession,
  invalidEmailDomains,
  loadPolicyStartSession,
  readPolicyStartRestore,
  readSuggestion,
  restoreFromDraft,
  savePolicyStartSession,
} from "./policy-draft";

const secretsPreset = {
  id: "secrets_and_credentials",
  label: "Secrets and credentials",
  description: "Block credentials.",
  policyType: "standard" as const,
  sources: ["gitleaks"],
  presidioEntities: [],
  action: "block" as const,
  score: 8,
  prompt: "",
  userMessage: "%{match} looks like a credential.",
  requiresApprovedEmailDomains: false,
};

describe("policy drafts", () => {
  it("expands a preset into a standard draft", () => {
    const draft = draftFromPreset(secretsPreset);
    expect(draft).toEqual({
      presetId: "secrets_and_credentials",
      policyType: "standard",
      name: "Secrets and credentials",
      action: "block",
      score: 8,
      sources: ["gitleaks"],
      presidioEntities: [],
      prompt: "",
      userMessage: "%{match} looks like a credential.",
      requiresApprovedEmailDomains: false,
      origin: { source: "preset" },
    });
    expect(draftKind(draft)).toBe("standard");
  });

  it("turns a bespoke suggestion into a prompt draft", () => {
    const draft = draftFromSuggestion({
      presetId: "",
      policyType: "prompt_based",
      name: "Refund Promises",
      action: "warn",
      score: 6,
      sources: [],
      presidioEntities: [],
      prompt: "Flag promises of refunds.",
      userMessage: "",
      rationale: "No preset covers this.",
      confidence: 0,
      alternatives: [],
    });
    expect(draft.policyType).toBe("prompt_based");
    expect(draft.prompt).toBe("Flag promises of refunds.");
    expect(draftKind(draft)).toBe("prompt");
  });

  it("reads only well-formed drafts out of router state", () => {
    expect(readPolicyDraft(null)).toBeNull();
    expect(readPolicyDraft({ draft: { policyType: "other" } })).toBeNull();
    expect(readPolicyDraft({ draft: { policyType: "standard" } })).toBeNull();

    const draft = readPolicyDraft({
      draft: {
        policyType: "standard",
        name: "Secrets",
        action: "nonsense",
        sources: ["gitleaks"],
      },
    });
    expect(draft).toEqual({
      presetId: "",
      policyType: "standard",
      name: "Secrets",
      action: "flag",
      score: 5,
      sources: ["gitleaks"],
      presidioEntities: [],
      prompt: "",
      userMessage: "",
    });
  });
});

describe("policy draft MCP scope", () => {
  const servers = [
    { id: "srv-github", name: "GitHub", slug: "github" },
    { id: "srv-pg", name: "Postgres (prod)", slug: "postgres-prod" },
    { id: "srv-hub", name: "Hub" },
  ];

  it("matches servers a description names on word boundaries", () => {
    expect(
      matchMCPServers("Block deletes on our GitHub server", servers).map(
        (server) => server.id,
      ),
    ).toEqual(["srv-github"]);
    expect(
      matchMCPServers("flag writes to postgres prod", servers).map(
        (server) => server.id,
      ),
    ).toEqual(["srv-pg"]);
    expect(matchMCPServers("stop deletes in production", servers)).toEqual([]);
  });

  it("marks session-level detectors as not scopable", () => {
    expect(sessionOnlyReason(["gitleaks"])).toBeNull();
    expect(sessionOnlyReason(["shadow_mcp"])).toContain("approved list");
    expect(sessionOnlyReason(["account_identity"])).toContain("signed in");
  });

  it("opens the scope step on the chosen servers", () => {
    const draft = {
      ...draftFromPreset(secretsPreset),
      mcpScope: { serverIds: ["srv-github"], matchedNames: ["GitHub"] },
    };
    expect(draftMCPScopeValue(draft)).toEqual({
      mode: "mcp",
      allServers: false,
      toolAnnotations: [],
      servers: [{ mcpServerId: "srv-github" }],
    });
    expect(draftMCPScopeValue(draftFromPreset(secretsPreset))).toBeNull();
    expect(
      draftMCPScopeValue({ ...draft, sources: ["shadow_mcp"] }),
    ).toBeNull();
  });

  it("keeps the scope through router state and drops malformed values", () => {
    const draft = {
      ...draftFromPreset(secretsPreset),
      mcpScope: { serverIds: ["srv-github"], matchedNames: ["GitHub"] },
    };
    expect(readPolicyDraft({ draft })?.mcpScope).toEqual(draft.mcpScope);
    expect(
      readPolicyDraft({ draft: { ...draft, mcpScope: { serverIds: [1] } } })
        ?.mcpScope,
    ).toEqual({ serverIds: [], matchedNames: [] });
    expect(
      readPolicyDraft({ draft: draftFromPreset(secretsPreset) })?.mcpScope,
    ).toBeUndefined();
  });
});

const refundSuggestion: SuggestRiskPolicyResult = {
  presetId: "",
  policyType: "prompt_based" as const,
  name: "Refund Promises",
  action: "flag",
  score: 5,
  sources: [],
  presidioEntities: [],
  prompt: "Flag promises of refunds.",
  userMessage: "",
  rationale: "No preset covers this.",
  confidence: 0,
  alternatives: [
    { presetId: "secrets_and_credentials", label: "Secrets", confidence: 0.5 },
  ],
};

describe("draft origin", () => {
  it("marks a preset draft as started from a use case", () => {
    expect(draftFromPreset(secretsPreset).origin).toEqual({ source: "preset" });
  });

  it("carries a described draft's origin through router state", () => {
    const draft = {
      ...draftFromSuggestion(refundSuggestion),
      origin: {
        source: "describe" as const,
        description: "never promise refunds",
        suggestion: refundSuggestion,
        overridePresetId: null,
      },
    };
    expect(readPolicyDraft({ draft })?.origin).toEqual(draft.origin);
  });

  it("drops an origin whose suggestion is malformed", () => {
    const draft = {
      ...draftFromSuggestion(refundSuggestion),
      origin: { source: "describe", description: "x", suggestion: {} },
    };
    expect(readPolicyDraft({ draft })?.origin).toBeUndefined();
    expect(readSuggestion({ presetId: "" })).toBeNull();
  });

  it("restores the review screen only for a described draft", () => {
    expect(restoreFromDraft(draftFromPreset(secretsPreset))).toEqual({
      description: "",
      review: null,
    });
    const restore = restoreFromDraft({
      ...draftFromSuggestion(refundSuggestion),
      origin: {
        source: "describe",
        description: "never promise refunds",
        suggestion: refundSuggestion,
        overridePresetId: "secrets_and_credentials",
      },
    });
    expect(readPolicyStartRestore({ restore })).toEqual(restore);
    expect(readPolicyStartRestore(null)).toBeNull();
  });
});

describe("invalidEmailDomains", () => {
  it("lists entries that are not hostnames", () => {
    expect(
      invalidEmailDomains(["acme.com", "corp.acme.co.uk", "acme..com", "acme"]),
    ).toEqual(["acme..com", "acme"]);
  });
});

describe("policy start session", () => {
  beforeEach(() => window.sessionStorage.clear());

  it("keeps a description and its review per project", () => {
    const restore = {
      description: "never promise refunds",
      review: { suggestion: refundSuggestion, overridePresetId: null },
    };
    savePolicyStartSession("default", restore);
    expect(loadPolicyStartSession("default")).toEqual(restore);
    expect(loadPolicyStartSession("other")).toBeNull();
  });

  it("drops the entry once there is nothing to restore", () => {
    savePolicyStartSession("default", { description: "draft", review: null });
    savePolicyStartSession("default", { description: "  ", review: null });
    expect(loadPolicyStartSession("default")).toBeNull();
  });

  it("clears after a policy is created", () => {
    savePolicyStartSession("default", { description: "draft", review: null });
    clearPolicyStartSession("default");
    expect(loadPolicyStartSession("default")).toBeNull();
  });
});
