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
  it("bounds long app names to Slack's 35-character limit without splitting Unicode characters", () => {
    for (const name of ["A".repeat(100), "🚀".repeat(50)]) {
      const manifest = slackAppManifest(
        [],
        "https://callback.example.test/callback",
        name,
      );
      expect(Array.from(manifest.display_information.name)).toHaveLength(35);
      expect(manifest.display_information.name).not.toMatch(
        /[\uD800-\uDBFF]$/u,
      );
    }
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
