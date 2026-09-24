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

    expect(
      renderVerb(
        log({
          action: "organization:product_feature_enabled",
          metadata: { feature_name: "sso" },
        }),
      ),
    ).toBe("enabled the sso feature for");

    expect(
      renderVerb(
        log({
          action: "organization:product_feature_disabled",
          metadata: { feature_name: "custom_model_keys" },
        }),
      ),
    ).toBe("disabled the custom model keys feature for");

    // Without metadata the fixed phrase still reads as a sentence.
    expect(
      renderVerb(log({ action: "organization:product_feature_disabled" })),
    ).toBe("disabled a product feature for");
  });

  // Every billing action is recorded against the same fixed subject display
  // name, so the phrase has to read as a sentence with it behind them.
  it("reads as a sentence in front of the billing metadata subject", () => {
    const sentence = (action: string) =>
      `${renderVerb(log({ action }))} Billing metadata`;

    expect(sentence("billing_metadata:create_stripe_portal")).toBe(
      "opened Stripe billing portal for Billing metadata",
    );
    expect(sentence("billing_metadata:cancel_stripe_subscription")).toBe(
      "canceled Stripe subscription for Billing metadata",
    );
    expect(sentence("billing_metadata:resume_stripe_subscription")).toBe(
      "resumed Stripe subscription for Billing metadata",
    );
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
    expect(
      formatAuditActionLabel("billing_metadata:cancel_stripe_subscription"),
    ).toBe("Canceled Stripe subscription");
    expect(
      formatAuditActionLabel("billing_metadata:create_stripe_portal"),
    ).toBe("Opened Stripe billing portal");
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

describe("past-tense fallback inflection", () => {
  it("inflects consonant-y verbs to -ied", () => {
    expect(renderVerb(log({ action: "widget:retry" }))).toBe("retried widget");
    expect(renderVerb(log({ action: "widget:deny" }))).toBe("denied widget");
    expect(renderVerb(log({ action: "widget:relay" }))).toBe("relayed widget");
  });
});
