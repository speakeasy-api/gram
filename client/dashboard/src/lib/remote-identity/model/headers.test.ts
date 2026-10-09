import { describe, expect, it } from "vitest";
import {
  isProtectedInboundHeader,
  passThroughAuthorizationProblem,
  remoteHeaderPolicyIssue,
  remoteHeaderPolicyReasonMessage,
} from "./headers";

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
      "Speakeasy-AI-Key",
      "speakeasy_ai_chat_session",
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
    ).toEqual({
      reason: "protected-source",
      field: "source",
      effect: "blocks-requests",
    });
  });

  it("refuses a custom header populated from Gram-Key", () => {
    expect(
      remoteHeaderPolicyIssue({
        name: "X-Upstream-Token",
        valueFromRequestHeader: "gram-key",
        isRequired: false,
      }),
    ).toEqual({
      reason: "protected-source",
      field: "source",
      effect: "suppressed",
    });
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
      field: "source",
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
    ).toEqual({
      reason: "reserved-name",
      field: "name",
      effect: "blocks-requests",
    });
    expect(
      remoteHeaderPolicyIssue({
        name: "Proxy-Authorization",
        isRequired: false,
      }),
    ).toEqual({ reason: "reserved-name", field: "name", effect: "suppressed" });
    expect(
      remoteHeaderPolicyIssue({
        name: "Cookie",
        valueFromRequestHeader: "X-Upstream-Cookie",
        isRequired: true,
      }),
    ).toEqual({
      reason: "reserved-name",
      field: "name",
      effect: "blocks-requests",
    });
  });

  it("calls rows naming protocol or assertion headers ignored, however required", () => {
    for (const name of [
      "Mcp-Method",
      "MCP_PROTOCOL_VERSION",
      "Mcp-Param-Region",
      "X-Speakeasy-Identity",
    ]) {
      expect(remoteHeaderPolicyIssue({ name, isRequired: true }), name).toEqual(
        { reason: "reserved-name", field: "name", effect: "ignored" },
      );
    }
    expect(
      remoteHeaderPolicyIssue({ name: "Mcp-Param-", isRequired: true }),
    ).toBeNull();
  });
});

describe("remoteHeaderPolicyIssue for stored rows", () => {
  it("fails malformed and padded names the way the proxy does", () => {
    expect(
      remoteHeaderPolicyIssue({ name: "X Bad", isRequired: true }),
    ).toEqual({
      reason: "invalid-name",
      field: "name",
      effect: "blocks-requests",
    });
    expect(
      remoteHeaderPolicyIssue({ name: " X-Api-Key ", isRequired: true }),
    ).toEqual({
      reason: "invalid-name",
      field: "name",
      effect: "blocks-requests",
    });
    expect(
      remoteHeaderPolicyIssue({
        name: "X-Upstream",
        valueFromRequestHeader: " X-Service-Token ",
        isRequired: false,
      }),
    ).toEqual({
      reason: "invalid-name",
      field: "source",
      effect: "suppressed",
    });
  });

  it("does not call a padded protocol header ignored, since the proxy rejects it", () => {
    expect(
      remoteHeaderPolicyIssue({ name: " Mcp-Method ", isRequired: true }),
    ).toEqual({
      reason: "invalid-name",
      field: "name",
      effect: "blocks-requests",
    });
  });

  it("keeps mixed casing and underscores usable", () => {
    for (const name of ["x-api-key", "X_API_KEY", "X-API-Key"]) {
      expect(remoteHeaderPolicyIssue({ name, isRequired: true }), name).toBe(
        null,
      );
    }
  });
});

describe("remoteHeaderPolicyIssue for writes", () => {
  it("allows padding the server trims, but not invalid names", () => {
    expect(
      remoteHeaderPolicyIssue(
        { name: " X-Api-Key ", isRequired: true },
        "write",
      ),
    ).toBeNull();
    expect(
      remoteHeaderPolicyIssue({ name: "X Bad", isRequired: true }, "write"),
    ).toMatchObject({ reason: "invalid-name", field: "name" });
    expect(
      remoteHeaderPolicyIssue(
        {
          name: "X-Upstream",
          valueFromRequestHeader: "X Bad",
          isRequired: true,
        },
        "write",
      ),
    ).toMatchObject({ reason: "invalid-name", field: "source" });
  });
});

describe("remoteHeaderPolicyReasonMessage", () => {
  it("offers upstream OAuth only for an Authorization destination", () => {
    expect(
      remoteHeaderPolicyReasonMessage("protected-source", {
        name: "Authorization",
        valueFromRequestHeader: "Authorization",
      }),
    ).toContain("connect the server's upstream OAuth");

    const custom = remoteHeaderPolicyReasonMessage("protected-source", {
      name: "X-Upstream-Token",
      valueFromRequestHeader: "Authorization",
    });
    expect(custom).not.toContain("connect the server's upstream OAuth");
    expect(custom).toContain("does not replace this header");
  });
});

describe("passThroughAuthorizationProblem", () => {
  function row(valueFromRequestHeader: string, isRequired: boolean) {
    return {
      id: "header-authorization",
      name: "Authorization",
      valueFromRequestHeader,
      isRequired,
      isSecret: false,
      createdAt: new Date(0),
      updatedAt: new Date(0),
    } as Parameters<typeof passThroughAuthorizationProblem>[0];
  }

  it("says a required row reading Authorization fails without an upstream account", () => {
    expect(passThroughAuthorizationProblem(row("Authorization", true))).toBe(
      "A pass-through Authorization header is still configured. Requests to this server fail unless a connected upstream account supplies Authorization.",
    );
  });

  it("says an optional row reading Authorization is not sent", () => {
    expect(
      passThroughAuthorizationProblem(row("Authorization", false)),
    ).toContain("Speakeasy does not send this header.");
  });

  it("only describes a row reading an allowed header", () => {
    expect(passThroughAuthorizationProblem(row("X-Service-Token", true))).toBe(
      "A pass-through Authorization header is still configured.",
    );
  });
});
