import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { describe, expect, it } from "vitest";
import {
  formatAuditActionLabel,
  formatSubjectLabel,
  renderVerb,
} from "./audit-log-format";

function log(partial: Partial<AuditLog> & { action: string }): AuditLog {
  return {
    id: "1",
    actorId: "actor",
    actorType: "user",
    subjectId: "subject",
    subjectType: "thing",
    createdAt: new Date(),
    ...partial,
  } as AuditLog;
}

describe("renderVerb", () => {
  it("humanizes known actions in past tense", () => {
    expect(renderVerb(log({ action: "risk_policy:delete" }))).toBe(
      "deleted risk policy",
    );
    expect(renderVerb(log({ action: "chat_session:access" }))).toBe(
      "opened chat session",
    );
    expect(renderVerb(log({ action: "skill:add_version" }))).toBe(
      "added a version to skill",
    );
  });

  it("falls back to a past-tense phrase for unknown actions", () => {
    expect(renderVerb(log({ action: "widget:delete" }))).toBe("deleted widget");
    expect(renderVerb(log({ action: "widget:force_sync" }))).toBe(
      "forced sync widget",
    );
    expect(renderVerb(log({ action: "widget:upsert" }))).toBe("updated widget");
  });

  it("describes what changed when the action alone is ambiguous", () => {
    expect(
      renderVerb(
        log({
          action: "toolset:update",
          beforeSnapshot: { Name: "old" },
          afterSnapshot: { Name: "new" },
        }),
      ),
    ).toBe("renamed MCP server");

    expect(
      renderVerb(
        log({
          action: "organization_invitation:create",
          metadata: { role_slug: "org_admin" },
        }),
      ),
    ).toBe("sent org admin invite to");
  });
});

describe("formatAuditActionLabel", () => {
  it("sentence-cases the phrase and drops the dangling preposition", () => {
    expect(formatAuditActionLabel("risk_policy:delete")).toBe(
      "Deleted risk policy",
    );
    expect(formatAuditActionLabel("organization_invitation:revoke")).toBe(
      "Revoked invite",
    );
    expect(formatAuditActionLabel("variation:update_global")).toBe(
      "Updated a global variation",
    );
    expect(formatAuditActionLabel("widget:delete")).toBe("Deleted widget");
  });
});

describe("formatSubjectLabel", () => {
  it("names the kind of a content-addressed upload", () => {
    expect(
      formatSubjectLabel(
        "functions-247f32b0fdbe3a8ac45116625705947fa4ad4c84d5cd956564a4f76e3f9b5abe.zip",
        "asset",
      ),
    ).toBe("Functions bundle · 247f32b0");
    expect(
      formatSubjectLabel(
        "openapi-aad67780a4bb1c9e0e2f4d1cb2a1b0c3d4e5f60718293a4b5c6d7e8f9abb08210fe9.yaml",
        "asset",
      ),
    ).toBe("OpenAPI document · aad67780");
  });

  it("labels bare UUIDs with their subject type", () => {
    expect(
      formatSubjectLabel("01900000-0000-7000-8000-000000000000", "deployment"),
    ).toBe("Deployment 01900000");
    expect(
      formatSubjectLabel("01900000-0000-7000-8000-000000000000", "mcp_server"),
    ).toBe("MCP server 01900000");
  });

  it("leaves human names alone", () => {
    expect(formatSubjectLabel("Shadow MCP Server Policy", "risk_policy")).toBe(
      "Shadow MCP Server Policy",
    );
    expect(formatSubjectLabel("ci-deploy", "api_key")).toBe("ci-deploy");
  });
});
