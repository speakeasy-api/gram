import { describe, expect, it } from "vitest";
import {
  buildServerGuardrailRequest,
  catalogPresetState,
  defaultServerGuardrailState,
  destructiveToolNames,
  effectiveAction,
  isDestructiveTool,
  serverGuardrailScope,
  validateServerGuardrail,
} from "./server-guardrail-policy";

const tools = [
  { name: "create_issue", destructive: false },
  { name: "delete_issue", destructive: true },
  { name: "archive_team", destructive: true },
];

describe("catalogPresetState", () => {
  it("logs secrets and PII, adding destructive tools when annotated", () => {
    const state = catalogPresetState(tools);
    expect([...state.categories].sort()).toEqual([
      "destructive_tool",
      "pii",
      "secrets",
    ]);
    expect(effectiveAction(state)).toBe("flag");
    expect(destructiveToolNames(tools)).toEqual([
      "delete_issue",
      "archive_team",
    ]);
  });

  it("warns on secrets and PII when no tool is destructive", () => {
    const state = catalogPresetState([{ name: "read", destructive: false }]);
    expect(state.categories.has("destructive_tool")).toBe(false);
    expect(effectiveAction(state)).toBe("warn");
  });
});

describe("isDestructiveTool", () => {
  it("ignores the destructive hint on read-only tools", () => {
    expect(isDestructiveTool({ destructiveHint: true })).toBe(true);
    expect(
      isDestructiveTool({ destructiveHint: true, readOnlyHint: true }),
    ).toBe(false);
    expect(isDestructiveTool({ readOnlyHint: false })).toBe(false);
    expect(isDestructiveTool(undefined)).toBe(false);
  });
});

describe("effectiveAction", () => {
  it("forces logging while a flag-only detector is selected", () => {
    const state = defaultServerGuardrailState();
    state.action = "warn";
    expect(effectiveAction(state)).toBe("warn");
    state.categories.add("destructive_tool");
    expect(effectiveAction(state)).toBe("flag");
  });
});

describe("serverGuardrailScope", () => {
  it("scopes to whole servers, or to sorted selected tools", () => {
    expect(
      serverGuardrailScope({ toolMode: "all", selectedTools: [] }, ["a", "b"]),
    ).toEqual({
      allServers: false,
      toolAnnotations: [],
      servers: [{ mcpServerId: "a" }, { mcpServerId: "b" }],
    });
    expect(
      serverGuardrailScope(
        { toolMode: "selected", selectedTools: ["z", "a"] },
        ["a"],
      ).servers,
    ).toEqual([{ mcpServerId: "a", tools: ["a", "z"] }]);
  });
});

describe("validateServerGuardrail", () => {
  it("requires a detector, tools when selected, and principals when targeted", () => {
    const state = defaultServerGuardrailState();
    expect(validateServerGuardrail(state)).toEqual({ ok: true });
    state.categories.clear();
    expect(validateServerGuardrail(state).ok).toBe(false);
    state.categories.add("secrets");
    state.toolMode = "selected";
    expect(validateServerGuardrail(state).ok).toBe(false);
    state.selectedTools = ["x"];
    state.audienceType = "targeted";
    expect(validateServerGuardrail(state).ok).toBe(false);
    state.audiencePrincipalUrns.add("role:admin");
    expect(validateServerGuardrail(state)).toEqual({ ok: true });
  });
});

describe("buildServerGuardrailRequest", () => {
  it("refuses an empty server list, which the backend reads as org-wide", () => {
    expect(() =>
      buildServerGuardrailRequest(defaultServerGuardrailState(), {
        mcpServerIds: [],
        name: "x",
        mode: "presidio",
      }),
    ).toThrow("at least one MCP server");
  });

  it("builds a scoped, enabled standard policy", () => {
    const state = catalogPresetState(tools);
    state.userMessage = "  careful  ";
    const request = buildServerGuardrailRequest(state, {
      mcpServerIds: ["srv"],
      name: " Linear guardrail ",
      mode: "presidio",
    });
    expect(request).toMatchObject({
      name: "Linear guardrail",
      policyType: "standard",
      enabled: true,
      action: "flag",
      score: 7,
      audienceType: "everyone",
      audiencePrincipalUrns: [],
      userMessage: "careful",
      mcpScope: {
        allServers: false,
        servers: [{ mcpServerId: "srv" }],
      },
    });
    expect(request.sources).toEqual(
      expect.arrayContaining(["gitleaks", "presidio", "destructive_tool"]),
    );
  });
});
