import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import { describe, expect, it } from "vitest";
import {
  draftFromTarget,
  draftToUpsertBody,
  emptyDraft,
  parseSignatureLines,
  signatureSummary,
  validateDraft,
} from "./aiScanTargetDraft";

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
  enabled: true,
  createdAt: new Date("2026-09-08T00:00:00Z"),
  updatedAt: new Date("2026-09-08T00:00:00Z"),
};

describe("parseSignatureLines", () => {
  it("splits on newlines only, trims, drops blanks and duplicates", () => {
    expect(
      parseSignatureLines(" claude \nclaude\ncodex\n\n \n~/Library/a,b "),
    ).toEqual(["claude", "codex", "~/Library/a,b"]);
  });
});

describe("validateDraft", () => {
  it("accepts a well-formed draft", () => {
    expect(validateDraft(draftFromTarget(classic))).toEqual({});
  });

  it("rejects the probes the device agent would refuse", () => {
    const base = draftFromTarget(classic);
    expect(validateDraft({ ...base, id: "ChatGPT" }).id).toBeDefined();
    expect(
      validateDraft({ ...base, displayName: " " }).displayName,
    ).toBeDefined();
    expect(
      validateDraft({ ...base, binaries: "../../etc/passwd" }).binaries,
    ).toContain("bare command name");
    expect(
      validateDraft({ ...base, configDirs: "~/" }).configDirs,
    ).toBeDefined();
    expect(
      validateDraft({ ...base, configDirs: "~/../.ssh" }).configDirs,
    ).toBeDefined();
    expect(
      validateDraft({ ...base, processNames: ".*" }).processNames,
    ).toBeDefined();
    expect(
      validateDraft({ ...base, versionPlistKey: "CF.Bundle" }).versionPlistKey,
    ).toBeDefined();
    expect(
      validateDraft({
        ...base,
        bundleIds: Array.from({ length: 17 }, (_, i) => `com.a.b${i}`).join(
          "\n",
        ),
      }).bundleIds,
    ).toContain("At most 16");
  });

  it("requires at least one install signature", () => {
    const errors = validateDraft({
      ...emptyDraft(),
      id: "x",
      displayName: "X",
      processNames: "X",
    });
    expect(errors.bundleIds).toContain("at least one install signature");
  });
});

describe("draftToUpsertBody", () => {
  it("normalizes the form into the request body", () => {
    const body = draftToUpsertBody({
      ...emptyDraft(),
      id: " chatgpt-classic ",
      displayName: " ChatGPT Classic ",
      category: "harness",
      bundleIds: "com.openai.chat\ncom.openai.chat",
      processNames: "ChatGPT",
      versionPlistKey: " ",
      enabled: false,
      reason: " customer ask ",
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
      reason: "customer ask",
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
