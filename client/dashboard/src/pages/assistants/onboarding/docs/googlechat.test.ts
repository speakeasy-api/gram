import { describe, expect, it } from "vitest";
import { getIntegrationDoc, listIntegrationDocs } from "./index";

describe("Google Chat setup documentation", () => {
  it("is discoverable and describes the native tool/trigger environment contract", () => {
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

  it("uses hosted shared-space setup and app authority for messages", () => {
    const body = getIntegrationDoc("googlechat")?.body ?? "";
    for (const requirement of [
      "Org > Identity > Google Chat",
      "Do not request service-account credentials or configure shared-space event delivery manually",
      "configure_google_chat_space",
      "mapped members can reply without repeating it",
      "chat.app.messages.readonly",
      "The user token is used for setup and membership sync",
      "It is not used to read messages or renew event subscriptions",
      "Google login alone cannot grant this app scope",
    ]) {
      expect(body).toContain(requirement);
    }
    expect(body).not.toContain("workspace_events");
    expect(body).not.toContain("auto_renew");
  });
});
