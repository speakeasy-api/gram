import { describe, expect, it } from "vitest";
import {
  draftFromPreset,
  draftFromSuggestion,
  draftKind,
  readPolicyDraft,
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
