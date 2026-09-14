import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import { describe, expect, it } from "vitest";
import {
  categoryLabel,
  draftFromTarget,
  draftToUpsertBody,
  emptyDraft,
  normalizeConfigDir,
  signatureSummary,
  slugFromName,
  validateDraft,
} from "./ai-scan-target-draft";

const classic: AiScanTarget = {
  id: "chatgpt-classic",
  displayName: "ChatGPT Classic",
  category: "harness",
  signatures: {
    bundleIds: ["com.openai.chat"],
    binaries: [],
    configDirs: [],
    processNames: [],
  },
  gatewayClient: {
    cimdVendorKeys: [],
    clientInfoNames: [],
    oauthClientIds: [],
  },
  enabled: true,
  origin: "default",
  customized: false,
  createdAt: new Date("2026-09-08T00:00:00Z"),
  updatedAt: new Date("2026-09-08T00:00:00Z"),
};

describe("slugFromName", () => {
  it("derives the id agents report from the display name", () => {
    expect(slugFromName("ChatGPT Classic")).toBe("chatgpt-classic");
    expect(slugFromName("  LM  Studio! ")).toBe("lm-studio");
    expect(slugFromName("Émacs 2.0")).toBe("emacs-2-0");
    expect(slugFromName("***")).toBe("");
    expect(slugFromName("x".repeat(80))).toHaveLength(64);
  });
});

describe("normalizeConfigDir", () => {
  it("only trims what was typed", () => {
    expect(normalizeConfigDir(" .claude ")).toBe(".claude");
    expect(normalizeConfigDir("~/.codex")).toBe("~/.codex");
    expect(
      normalizeConfigDir("~/Library/Application Support/com.openai.chat/"),
    ).toBe("~/Library/Application Support/com.openai.chat/");
    expect(normalizeConfigDir("/opt/homebrew/etc/claude")).toBe(
      "/opt/homebrew/etc/claude",
    );
    expect(normalizeConfigDir("  ")).toBe("");
  });
});

describe("validateDraft", () => {
  it("accepts a well-formed draft", () => {
    expect(validateDraft(draftFromTarget(classic))).toEqual({});
  });

  it("rejects the probes the device agent would refuse", () => {
    const base = draftFromTarget(classic);
    expect(validateDraft({ ...base, id: "ChatGPT" }).id).toBeDefined();
    expect(validateDraft({ ...base, id: "" }).id).toContain("letter or digit");
    expect(
      validateDraft({ ...base, displayName: " " }).displayName,
    ).toBeDefined();
    expect(
      validateDraft({ ...base, binaries: ["../../etc/passwd"] }).binaries,
    ).toContain("bare command name");
    // A config dir is any path the agent can resolve, so nothing about its
    // shape is rejected here.
    for (const dir of [
      ".claude",
      "~/.claude/",
      "/opt/../etc",
      "~/Library/Application Support/com.openai.chat/",
    ]) {
      expect(
        validateDraft({ ...base, configDirs: [dir] }).configDirs,
      ).toBeUndefined();
    }
    expect(
      validateDraft({ ...base, processNames: [".*"] }).processNames,
    ).toBeDefined();
    expect(
      validateDraft({ ...base, versionPlistKey: "CF.Bundle" }).versionPlistKey,
    ).toBeDefined();
    expect(
      validateDraft({
        ...base,
        bundleIds: Array.from({ length: 17 }, (_, i) => `com.a.b${i}`),
      }).bundleIds,
    ).toContain("At most 16");
  });

  it("requires at least one install signature", () => {
    const errors = validateDraft({
      ...emptyDraft(),
      id: "x",
      displayName: "X",
      processNames: ["X"],
    });
    expect(errors.binaries).toContain("at least one install signature");
  });
});

describe("draftToUpsertBody", () => {
  it("normalizes the form into the request body", () => {
    const body = draftToUpsertBody({
      ...emptyDraft(),
      id: " chatgpt-classic ",
      displayName: " ChatGPT Classic ",
      category: "harness",
      bundleIds: ["com.openai.chat"],
      processNames: ["ChatGPT"],
      versionPlistKey: " ",
      enabled: false,
    });
    expect(body).toEqual({
      id: "chatgpt-classic",
      displayName: "ChatGPT Classic",
      category: "harness",
      signatures: {
        bundleIds: ["com.openai.chat"],
        binaries: [],
        configDirs: [],
        processNames: ["ChatGPT"],
      },
      versionPlistKey: undefined,
      enabled: false,
    });
  });
});

describe("signatureSummary", () => {
  it("counts each signature kind", () => {
    expect(signatureSummary(classic)).toBe("1 bundle id");
    expect(
      signatureSummary({
        ...classic,
        signatures: {
          bundleIds: [],
          binaries: ["claude"],
          configDirs: ["~/.claude", "~/.config/claude"],
          processNames: ["claude"],
        },
      }),
    ).toBe("1 binary · 2 config dirs · 1 process name");
  });
});

describe("draftFromTarget", () => {
  // Editing or toggling a target round-trips it through the draft, so a
  // category the draft cannot represent is silently rewritten on the next
  // upsert. That reclassified every assistant target as a harness.
  it("carries every category through unchanged", () => {
    for (const category of ["harness", "assistant", "local_model"] as const) {
      expect(draftFromTarget({ ...classic, category }).category).toBe(category);
    }
  });

  it("round-trips a category through the upsert body", () => {
    const body = draftToUpsertBody(
      draftFromTarget({ ...classic, category: "assistant" }),
    );
    expect(body.category).toBe("assistant");
  });
});

describe("categoryLabel", () => {
  it("labels every category the editor offers", () => {
    expect(categoryLabel("harness")).toBe("Harness");
    expect(categoryLabel("assistant")).toBe("Assistant");
    expect(categoryLabel("local_model")).toBe("Local model");
  });

  it("falls back to the raw value for an unknown category", () => {
    expect(categoryLabel("something-new")).toBe("something-new");
  });
});
