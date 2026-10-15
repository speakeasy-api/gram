import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { encodeBasicCredential } from "../model/credential";
import { useAgentCredentialFields } from "./useCredentialDraft";

function header(value: string): RemoteMcpServerHeader {
  return {
    id: "header-authorization",
    name: "Authorization",
    value,
    isRequired: true,
    isSecret: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  } as RemoteMcpServerHeader;
}

afterEach(() => {
  cleanup();
});

describe("useAgentCredentialFields", () => {
  it("loads a readable Basic credential into its two fields", () => {
    const value = `Basic ${encodeBasicCredential("svc", "pa:ss")}`;
    const { result } = renderHook(() =>
      useAgentCredentialFields(header(value)),
    );

    expect(result.current.format).toBe("basic");
    expect(result.current.username).toBe("svc");
    expect(result.current.password).toBe("pa:ss");
    // Loaded as saved, so it is complete and unchanged.
    expect(result.current.authorizationValue).toBe(value);
    expect(result.current.isValid).toBe(true);
    expect(result.current.isDirty).toBe(false);
  });

  it("seeds nothing from a redacted secret", () => {
    const { result } = renderHook(() =>
      useAgentCredentialFields(header("***")),
    );

    expect(result.current.format).toBe("bearer");
    expect(result.current.token).toBe("");
    expect(result.current.isValid).toBe(false);
  });
});
