import { describe, expect, it } from "vitest";
import { slackAppManifest, slackAppCreationUrl } from "./slack";
import { initialState } from "./state";
import { scopesFor } from "./provider-data";

describe("Slack app creation handoff", () => {
  it("uses Slack's documented manifest link with selected user scopes and the actual callback", () => {
    const state = initialState("slack");
    state.values.access = "read";
    const manifest = slackAppManifest(
      scopesFor(state),
      "https://callback.example.test/mcp/remote_login_callback",
      "Team Slack",
    );
    const url = new URL(slackAppCreationUrl(manifest));
    expect(url.origin).toBe("https://api.slack.com");
    expect(url.pathname).toBe("/apps");
    expect(url.searchParams.get("new_app")).toBe("1");
    expect(manifest.display_information.name).toBe("Speakeasy · Team Slack");
    expect(JSON.parse(url.searchParams.get("manifest_json")!)).toEqual(
      manifest,
    );
    expect(manifest.oauth_config.redirect_urls).toEqual([
      "https://callback.example.test/mcp/remote_login_callback",
    ]);
    expect(manifest.oauth_config.scopes.user).not.toContain("chat:write");
    expect(manifest.settings.is_mcp_enabled).toBe(true);
  });
  it("preserves write scopes when selected and contains no credentials", () => {
    const manifest = slackAppManifest(
      scopesFor(initialState("slack")),
      "https://callback.example.test/mcp/remote_login_callback",
      "Support Slack",
    );
    expect(manifest.oauth_config.scopes.user).toContain("chat:write");
    expect(JSON.stringify(manifest)).not.toMatch(
      /client_secret|client_id|EXAMPLE_SECRET|example.speakeasy.com/,
    );
  });
});
