type SlackAppManifest = {
  display_information: { name: string };
  oauth_config: { redirect_urls: string[]; scopes: { user: string[] } };
  settings: { is_mcp_enabled: boolean };
};

/** Slack's documented shareable manifest link; creation is confirmed in Slack. */
export function slackAppManifest(
  scopes: readonly string[],
  callbackUrl: string,
  serverName: string,
): SlackAppManifest {
  return {
    display_information: { name: `Speakeasy · ${serverName}` },
    oauth_config: {
      redirect_urls: [callbackUrl],
      scopes: { user: [...scopes] },
    },
    settings: { is_mcp_enabled: true },
  };
}
export function slackAppCreationUrl(
  manifest: ReturnType<typeof slackAppManifest>,
): string {
  const url = new URL("https://api.slack.com/apps");
  url.searchParams.set("new_app", "1");
  url.searchParams.set("manifest_json", JSON.stringify(manifest));
  return url.toString();
}
