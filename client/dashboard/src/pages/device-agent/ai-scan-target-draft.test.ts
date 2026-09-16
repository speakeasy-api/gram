import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";
import { describe, expect, it } from "vitest";
import {
  categoryLabel,
  clientIdFromCimdInput,
  draftFromTarget,
  draftToUpsertBody,
  emptyDraft,
  normalizeConfigDir,
  signatureSummary,
  slugFromName,
  validateDraft,
  withCategory,
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
    oauthClientIds: [],
    clientInfoNames: [],
  },
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

  // The same shape rules the server applies to a stored client id, so a
  // draft that passes here is not refused on save for its matchers.
  it("holds a client id to the shape the server accepts", () => {
    const base = draftFromTarget(classic);
    expect(
      validateDraft({
        ...base,
        oauthClientIds: ["https://client.example/oauth/client-metadata.json"],
      }).oauthClientIds,
    ).toBeUndefined();
    expect(
      validateDraft({ ...base, oauthClientIds: ["https://client.example"] })
        .oauthClientIds,
    ).toContain("must include a path");
    expect(
      validateDraft({
        ...base,
        oauthClientIds: ["https://client.example/a#fragment"],
      }).oauthClientIds,
    ).toContain("fragment");
  });
});

describe("withCategory", () => {
  const withMatchers = {
    ...draftFromTarget(classic),
    cimdVendorKeys: ["vendor"],
    oauthClientIds: ["https://client.example/oauth/client-metadata.json"],
    clientInfoNames: ["client"],
  };

  // The form hides the matcher fields for an open model, so matchers left
  // behind by a switch could be neither seen nor cleared — and would block
  // the save with an error the form has nowhere to show.
  it("drops the gateway matchers on a switch to a kind that never calls the gateway", () => {
    const switched = withCategory(withMatchers, "local_model");
    expect(switched.category).toBe("local_model");
    expect(switched.cimdVendorKeys).toEqual([]);
    expect(switched.oauthClientIds).toEqual([]);
    expect(switched.clientInfoNames).toEqual([]);
    expect(validateDraft(switched)).toEqual({});
  });

  it("keeps the matchers on a switch between kinds that call the gateway", () => {
    const switched = withCategory(withMatchers, "assistant");
    expect(switched.category).toBe("assistant");
    expect(switched.cimdVendorKeys).toEqual(withMatchers.cimdVendorKeys);
    expect(switched.oauthClientIds).toEqual(withMatchers.oauthClientIds);
    expect(switched.clientInfoNames).toEqual(withMatchers.clientInfoNames);
  });
});

describe("clientIdFromCimdInput", () => {
  const url = "https://client.example/oauth/client-metadata.json";

  it("takes the document URL, or the document whose client_id is that URL", () => {
    expect(clientIdFromCimdInput(` ${url} `)).toEqual({ clientId: url });
    expect(
      clientIdFromCimdInput(
        JSON.stringify({ client_id: url, client_name: "Client" }),
      ),
    ).toEqual({ clientId: url });
  });

  it("explains what a document without a usable client_id is missing", () => {
    expect(clientIdFromCimdInput("")).toHaveProperty("error");
    expect(clientIdFromCimdInput("not a url")).toHaveProperty("error");
    expect(clientIdFromCimdInput("{")).toHaveProperty("error");
    expect(clientIdFromCimdInput("[]")).toHaveProperty("error");
    expect(clientIdFromCimdInput("{}")).toHaveProperty("error");
    expect(
      clientIdFromCimdInput(
        JSON.stringify({ client_id: "http://x.example/a" }),
      ),
    ).toHaveProperty("error");
  });

  // Mirrors the server's validateClientIDURLShape: every one of these is
  // refused on save, so it is refused before it reaches the list.
  it("refuses a client id the server would refuse", () => {
    const refused: Array<[string, string]> = [
      ["https://client.example", "must include a path"],
      ["https://client.example?x=1", "must include a path"],
      ["https:///client-metadata.json", "must include a host"],
      ["https://user@client.example/a", "userinfo"],
      ["https://client.example/a#fragment", "fragment"],
      ["https://client.example/./a", '"." or ".."'],
      ["https://client.example/a/../b", '"." or ".."'],
      ["https://client.example/%2e%2e/a", '"." or ".."'],
      // Go decodes the whole path before splitting it, so an encoded slash
      // separates segments there; the client has to split the same way.
      ["https://client.example/a%2F..%2Fb", '"." or ".."'],
      // A C1 control, which unicode.IsControl rejects alongside the C0 range.
      ["https://client.example/a\u0085b", "spaces"],
      ["https://client.example/%gh", "parseable"],
      ["https://[bad/a", "parseable"],
    ];
    for (const [input, problem] of refused) {
      // Whether typed as the URL or carried by a pasted document.
      const typed = clientIdFromCimdInput(input);
      expect(typed, input).toHaveProperty("error");
      expect((typed as { error: string }).error, input).toContain(problem);
      const pasted = clientIdFromCimdInput(
        JSON.stringify({ client_id: input }),
      );
      expect(pasted, input).toHaveProperty("error");
      expect((pasted as { error: string }).error, input).toContain(problem);
    }
  });

  it("accepts the shapes the server accepts", () => {
    for (const input of [
      url,
      "https://client.example/",
      "https://client.example/a?x=1",
      "https://client.example:8443/a/b.json",
      "https://client.example/a%20b/c",
    ]) {
      expect(clientIdFromCimdInput(input), input).toEqual({ clientId: input });
    }
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
      gatewayClient: {
        cimdVendorKeys: [],
        oauthClientIds: [],
        clientInfoNames: [],
      },
      versionPlistKey: undefined,
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
  // Editing a target round-trips it through the draft, so a
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
    expect(categoryLabel("local_model")).toBe("Open model");
  });

  it("falls back to the raw value for an unknown category", () => {
    expect(categoryLabel("something-new")).toBe("something-new");
  });
});
