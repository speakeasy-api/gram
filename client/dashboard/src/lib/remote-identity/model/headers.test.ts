import { describe, expect, it } from "vitest";
import { isProtectedInboundHeader, remoteHeaderPolicyIssue } from "./headers";

describe("isProtectedInboundHeader", () => {
  it("covers every Speakeasy credential and transport family", () => {
    for (const name of [
      "Authorization",
      "proxy-authorization",
      "Cookie",
      "Set-Cookie",
      "Gram-Key",
      "gram-chat-session",
      "GRAM_SESSION",
      "Gram-Project",
      "Gram-Consent-State",
      "X-Gram-Tunnel-Id",
      "X-Gram-Agent-Version",
      "X_Speakeasy_Identity",
    ]) {
      expect(isProtectedInboundHeader(name), name).toBe(true);
    }
  });

  it("leaves ordinary headers alone", () => {
    for (const name of ["X-Service-Token", "X-Upstream-Token", "Grammar"]) {
      expect(isProtectedInboundHeader(name), name).toBe(false);
    }
  });
});

describe("remoteHeaderPolicyIssue", () => {
  it("refuses a custom header populated from Authorization", () => {
    expect(
      remoteHeaderPolicyIssue({
        name: "X-Upstream-Token",
        valueFromRequestHeader: "Authorization",
        isRequired: true,
      }),
    ).toEqual({ reason: "protected-source", effect: "blocks-requests" });
  });

  it("refuses a custom header populated from Gram-Key", () => {
    expect(
      remoteHeaderPolicyIssue({
        name: "X-Upstream-Token",
        valueFromRequestHeader: "gram-key",
        isRequired: false,
      }),
    ).toEqual({ reason: "protected-source", effect: "suppressed" });
  });

  it("lets a resolved upstream token stand in for a required Authorization row", () => {
    expect(
      remoteHeaderPolicyIssue({
        name: "Authorization",
        valueFromRequestHeader: "Authorization",
        isRequired: true,
      }),
    ).toEqual({
      reason: "protected-source",
      effect: "blocks-unless-upstream-token",
    });
  });

  it("allows Authorization populated from a separately supplied header", () => {
    expect(
      remoteHeaderPolicyIssue({
        name: "Authorization",
        valueFromRequestHeader: "X-Service-Token",
        isRequired: true,
      }),
    ).toBeNull();
  });

  it("allows operator credentials under Cookie, Gram-Key and Authorization", () => {
    for (const name of ["Cookie", "Gram-Key", "Authorization", "X-Api-Key"]) {
      expect(remoteHeaderPolicyIssue({ name, isRequired: true }), name).toBe(
        null,
      );
    }
  });

  it("refuses reserved destinations", () => {
    expect(
      remoteHeaderPolicyIssue({ name: "Set-Cookie", isRequired: true }),
    ).toEqual({ reason: "reserved-name", effect: "blocks-requests" });
    expect(
      remoteHeaderPolicyIssue({
        name: "Proxy-Authorization",
        isRequired: false,
      }),
    ).toEqual({ reason: "reserved-name", effect: "suppressed" });
    expect(
      remoteHeaderPolicyIssue({
        name: "Cookie",
        valueFromRequestHeader: "X-Upstream-Cookie",
        isRequired: true,
      }),
    ).toEqual({ reason: "reserved-name", effect: "blocks-requests" });
  });

  it("calls rows naming protocol or assertion headers ignored, however required", () => {
    for (const name of [
      "Mcp-Method",
      "MCP_PROTOCOL_VERSION",
      "Mcp-Param-Region",
      "X-Speakeasy-Identity",
    ]) {
      expect(remoteHeaderPolicyIssue({ name, isRequired: true }), name).toEqual(
        { reason: "reserved-name", effect: "ignored" },
      );
    }
    expect(
      remoteHeaderPolicyIssue({ name: "Mcp-Param-", isRequired: true }),
    ).toBeNull();
  });
});
