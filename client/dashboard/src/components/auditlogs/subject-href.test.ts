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

  it("returns the unknown-subject fallback without using metadata", () => {
    expect(subjectHref(log("future_subject"), "example")).toBeNull();
  });
});
