import { describe, expect, it } from "vitest";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { subjectHref } from "./subject-href";

function log(subjectType: string, subjectId = "prescription-1"): AuditLog {
  return {
    id: "audit-1",
    actingSurface: "dashboard",
    action: "killswitch:activate",
    actorId: "actor-1",
    actorType: "user",
    createdAt: new Date("2026-01-01T00:00:00Z"),
    subjectId,
    subjectType,
    metadata: { internal_note: "must-not-enter-a-link" },
  };
}

describe("audit subject links", () => {
  it("links a Killswitch prescription directly by stable subject id", () => {
    expect(subjectHref(log("killswitch_prescription"), "example")).toBe(
      "/example/killswitch/prescription-1",
    );
  });

  it("links the Speakeasy issuer and its keys to the Speakeasy provider page", () => {
    const speakeasy =
      "/example/identity?tab=identity-providers&provider=speakeasy";
    expect(subjectHref(log("internal_session_issuer"), "example")).toBe(
      speakeasy,
    );
    for (const subjectType of ["json_web_key", "json_web_key_set"]) {
      expect(
        subjectHref(
          {
            ...log(subjectType, "set-1"),
            metadata: {
              internal_session_issuer: true,
              json_web_key_set_id: "set-1",
            },
          },
          "example",
        ),
      ).toBe(speakeasy);
    }
    expect(
      subjectHref(
        {
          ...log("json_web_key", "key-1"),
          metadata: { json_web_key_set_id: "set-1" },
        },
        "example",
      ),
    ).toBe("/example/signing-keys/set-1/keys");
  });

  it("links built-in updates to their own project and key", () => {
    const builtin = {
      ...log("builtin_mcp_server", "synthetic-id"),
      projectSlug: "other-project",
      subjectSlug: "slack",
    };
    expect(subjectHref(builtin, "example")).toBe(
      "/example/projects/other-project/mcp/builtin/slack",
    );
    expect(
      subjectHref({ ...builtin, projectSlug: undefined }, "example"),
    ).toBeNull();
    expect(
      subjectHref({ ...builtin, subjectSlug: undefined }, "example"),
    ).toBeNull();
  });

  it("returns the unknown-subject fallback without using metadata", () => {
    expect(subjectHref(log("future_subject"), "example")).toBeNull();
  });
});
