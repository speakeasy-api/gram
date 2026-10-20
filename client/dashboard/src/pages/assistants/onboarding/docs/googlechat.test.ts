import { describe, expect, it } from "vitest";
import { getIntegrationDoc, listIntegrationDocs } from "./index";

describe("Google Chat setup documentation", () => {
  it("is discoverable and describes the shared tool/trigger environment contract", () => {
    expect(listIntegrationDocs()).toContainEqual(
      expect.objectContaining({ slug: "googlechat", title: "Google Chat" }),
    );
    const body = getIntegrationDoc("googlechat")?.body ?? "";
    for (const contract of [
      "tools:platform:googlechat:",
      "attach_toolset",
      "environment_slug",
      "environment_id",
      "GOOGLE_CHAT_PROJECT_NUMBER",
      "GOOGLE_CHAT_SERVICE_ACCOUNT_JSON",
      "show_google_chat_app_guide",
      "request_environment_secrets",
      "sensitive:true",
    ]) {
      expect(body).toContain(contract);
    }
  });

  it("collects both credentials before creating a trigger and showing its URL", () => {
    const body = getIntegrationDoc("googlechat")?.body ?? "";
    const credentials = body.indexOf("4. Use add_environment_keys");
    const trigger = body.indexOf(
      "5. Only after BOTH values are populated, call create_trigger",
    );
    const guide = body.indexOf(
      "6. Only after trigger creation succeeds, show_google_chat_app_guide",
    );
    expect(credentials).toBeGreaterThan(-1);
    expect(trigger).toBeGreaterThan(credentials);
    expect(guide).toBeGreaterThan(trigger);
    expect(body.slice(credentials, trigger)).toContain(
      "request_environment_secrets",
    );
    expect(body.slice(credentials, trigger)).toContain("saved_keys");
    expect(body.slice(credentials, trigger)).toContain("populated_entry_names");
    expect(body.slice(credentials, trigger)).toContain(
      "If skipped, cancelled, failed, or either value is missing, stop setup",
    );
  });

  it("distinguishes non-add-on ingress and approved on-demand history", () => {
    const body = getIntegrationDoc("googlechat")?.body ?? "";
    for (const requirement of [
      "Project Number",
      "not a Google Workspace add-on",
      "chat@system.gserviceaccount.com",
      "Marketplace-compatible OAuth client",
      "Individual + admin install",
      "chat.app.messages.readonly",
      "Do not add `chat.bot`",
      "unmentioned",
      "No user OAuth",
    ]) {
      expect(body).toContain(requirement);
    }
  });
});
