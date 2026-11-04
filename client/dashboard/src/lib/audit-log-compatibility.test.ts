import { describe, expect, it } from "vitest";
import { AuditLog$inboundSchema } from "@gram/client/models/components/auditlog.js";
import * as z from "zod/v4-mini";

describe("audit log compatibility", () => {
  it.each([false, true, undefined])("decodes is_internal=%s", (isInternal) => {
    const log = z.parse(AuditLog$inboundSchema, {
      id: "event",
      actor_id: "actor",
      actor_type: "user",
      action: "project:update",
      acting_surface: "dashboard",
      created_at: "2026-03-12T15:30:00Z",
      subject_id: "project",
      subject_type: "project",
      ...(isInternal === undefined ? {} : { is_internal: isInternal }),
    });
    expect(log.isInternal).toBe(isInternal);
  });
});
