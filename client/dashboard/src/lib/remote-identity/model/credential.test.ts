import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { describe, expect, it } from "vitest";
import {
  EMPTY_AGENT_CREDENTIAL,
  credentialFormatFromHeader,
  credentialFromHeader,
  credentialPreview,
  credentialToAuthorizationValue,
  encodeBasicCredential,
} from "./credential";
import { REDACTED_SECRET } from "./secret";

function header(value: string | undefined): RemoteMcpServerHeader {
  return {
    id: "header-1",
    name: "Authorization",
    value,
    isRequired: true,
    isSecret: true,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  } as RemoteMcpServerHeader;
}

describe("credentialFormatFromHeader", () => {
  it("starts an unconfigured server on Bearer, not Manual", () => {
    // Manual would ask an operator to hand-assemble a header before they have
    // seen the simpler options.
    expect(credentialFormatFromHeader(undefined)).toBe("bearer");
  });

  it("reads the scheme off a readable value", () => {
    expect(credentialFormatFromHeader(header("Bearer abc"))).toBe("bearer");
    expect(credentialFormatFromHeader(header("Basic dXNlcjpwdw=="))).toBe(
      "basic",
    );
    expect(credentialFormatFromHeader(header("Token abc123"))).toBe("manual");
  });

  it("treats a redacted value as Bearer, since it says nothing", () => {
    expect(credentialFormatFromHeader(header(REDACTED_SECRET))).toBe("bearer");
  });
});

describe("credentialFromHeader", () => {
  it("seeds a readable bearer token", () => {
    expect(credentialFromHeader(header("Bearer abc"))).toMatchObject({
      format: "bearer",
      token: { kind: "set", value: "abc" },
    });
  });

  it("seeds nothing from a redacted value", () => {
    // The server is holding a credential it will not show us. That is
    // "unchanged", not an empty field the operator cleared.
    expect(credentialFromHeader(header(REDACTED_SECRET))).toMatchObject({
      format: "bearer",
      token: { kind: "unchanged" },
    });
  });
});

describe("credentialToAuthorizationValue", () => {
  it("is null until the credential can actually be sent", () => {
    expect(credentialToAuthorizationValue(EMPTY_AGENT_CREDENTIAL)).toBeNull();
    expect(
      credentialToAuthorizationValue({
        ...EMPTY_AGENT_CREDENTIAL,
        format: "basic",
        username: "svc",
      }),
    ).toBeNull();
  });

  it("assembles the scheme it was given", () => {
    expect(
      credentialToAuthorizationValue({
        ...EMPTY_AGENT_CREDENTIAL,
        token: { kind: "set", value: "abc" },
      }),
    ).toBe("Bearer abc");
    expect(
      credentialToAuthorizationValue({
        ...EMPTY_AGENT_CREDENTIAL,
        prefix: "  ",
        token: { kind: "set", value: "abc" },
      }),
    ).toBe("abc");
    expect(
      credentialToAuthorizationValue({
        ...EMPTY_AGENT_CREDENTIAL,
        format: "basic",
        username: "user",
        password: { kind: "set", value: "pw" },
      }),
    ).toBe(`Basic ${encodeBasicCredential("user", "pw")}`);
  });

  it("never assembles the format that is not built", () => {
    expect(
      credentialToAuthorizationValue({
        ...EMPTY_AGENT_CREDENTIAL,
        format: "client-credentials",
        token: { kind: "set", value: "abc" },
      }),
    ).toBeNull();
  });
});

describe("credentialPreview", () => {
  it("shows the shape before anything is entered", () => {
    expect(credentialPreview(EMPTY_AGENT_CREDENTIAL, false)).toBe(
      "Bearer <token>",
    );
    expect(
      credentialPreview({ ...EMPTY_AGENT_CREDENTIAL, format: "basic" }, false),
    ).toBe("Basic <base64(username:password)>");
  });

  it("masks by default and never leaks the value", () => {
    const credential = {
      ...EMPTY_AGENT_CREDENTIAL,
      token: { kind: "set" as const, value: "super-secret" },
    };
    const masked = credentialPreview(credential, false);
    expect(masked).not.toContain("super-secret");
    expect(masked).toContain("•");
    expect(credentialPreview(credential, true)).toBe("Bearer super-secret");
  });

  it("caps the mask so a long token cannot wrap the preview", () => {
    const credential = {
      ...EMPTY_AGENT_CREDENTIAL,
      token: { kind: "set" as const, value: "x".repeat(200) },
    };
    expect(credentialPreview(credential, false)).toBe(
      `Bearer ${"•".repeat(28)}`,
    );
  });
});
