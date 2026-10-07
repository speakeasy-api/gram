// Claude Code install copy and settings snippet, shared by the plugin, hooks,
// setup, and Platform MCP flows so they never drift.

/** Registering a marketplace and enabling its plugins from settings. */
export const CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL =
  "https://code.claude.com/docs/en/plugins/org#require-a-marketplace-and-its-plugins";

/**
 * Claude Code applies autoUpdate only from the extraKnownMarketplaces entry
 * keyed by the marketplace.json name, and installs enabledPlugins entries only
 * under that same name, so every snippet carries this one-line reminder.
 */
export const CLAUDE_CODE_EXACT_NAME_NOTE = "Use this exact marketplace name.";

/**
 * The settings block that registers a marketplace and enables its plugins.
 * Claude Code reads the same keys from ~/.claude/settings.json and from
 * managed settings, so one snippet serves both. With autoUpdate on there is
 * no `/plugin marketplace add` or `/plugin install` step: Claude Code clones
 * the marketplace and loads the plugins at the next start.
 */
export function claudeCodeSettingsJson({
  marketplaceName,
  marketplaceUrl,
  plugins,
  env = {},
}: {
  marketplaceName: string;
  marketplaceUrl: string;
  plugins: string[];
  env?: Record<string, string>;
}): string {
  return JSON.stringify(
    {
      // Keeps plugin auto-update running when DISABLE_AUTOUPDATER stops
      // Claude Code's own updates.
      env: { ...env, FORCE_AUTOUPDATE_PLUGINS: "1" },
      extraKnownMarketplaces: {
        [marketplaceName]: {
          autoUpdate: true,
          source: { source: "git", url: marketplaceUrl },
        },
      },
      enabledPlugins: Object.fromEntries(
        plugins.map((plugin) => [`${plugin}@${marketplaceName}`, true]),
      ),
    },
    null,
    2,
  );
}
