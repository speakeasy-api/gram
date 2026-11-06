// @vitest-environment node
import { describe, expect, it } from "vitest";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { denialFromError, denialMessage } from "./denial";

function gramError(status: number, headers: Record<string, string>): GramError {
  const body = '{"name":"forbidden","message":"permission denied"}';
  return new GramError("permission denied", {
    response: new Response(body, { status, headers }),
    request: new Request("https://api.example.test/rpc/telemetry.logs"),
    body,
  });
}

describe("denialFromError", () => {
  it("reads the scope and resource kind the server named", () => {
    expect(
      denialFromError(
        gramError(403, {
          "Gram-Required-Scope": "log:read",
          "Gram-Required-Resource-Kind": "project",
        }),
      ),
    ).toEqual({ scope: "log:read", resourceKind: "project" });
  });

  it("ignores a 403 the authz engine did not produce", () => {
    expect(denialFromError(gramError(403, {}))).toBeNull();
  });

  it("ignores other statuses and other errors", () => {
    expect(
      denialFromError(gramError(404, { "Gram-Required-Scope": "log:read" })),
    ).toBeNull();
    expect(denialFromError(new Error("boom"))).toBeNull();
    expect(denialFromError(undefined)).toBeNull();
  });
});

describe("denialMessage", () => {
  it("names the permission and where it is needed", () => {
    expect(
      denialMessage(
        { scope: "log:read", resourceKind: "project" },
        "View tool logs",
      ),
    ).toBe(
      "You need the View tool logs (log:read) permission on this project. An organization admin can grant it.",
    );
  });

  it("names the risk policy and inference kinds", () => {
    expect(
      denialMessage({
        scope: "risk_policy:evaluate",
        resourceKind: "risk_policy",
      }),
    ).toContain("on this risk policy.");
    expect(
      denialMessage({ scope: "inference:execute", resourceKind: "inference" }),
    ).toContain("on this project.");
  });

  it("falls back to the slug and a generic place", () => {
    expect(denialMessage({ scope: "log:read", resourceKind: "" })).toBe(
      "You need the log:read permission on this resource. An organization admin can grant it.",
    );
  });
});
