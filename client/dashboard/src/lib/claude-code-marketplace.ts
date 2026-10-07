// Claude Code docs links and note copy shown beside every snippet or command
// that registers a Speakeasy marketplace, shared by the plugin, hooks, and
// setup flows so they never drift.

/** The extraKnownMarketplaces key and enabledPlugins suffix are the marketplace.json name. */
export const CLAUDE_CODE_REQUIRE_MARKETPLACE_DOCS_URL =
  "https://code.claude.com/docs/en/plugins/org#require-a-marketplace-and-its-plugins";

/** A marketplace added with /plugin marketplace add has auto-update off until someone turns it on. */
export const CLAUDE_CODE_MARKETPLACE_AUTO_UPDATE_DOCS_URL =
  "https://code.claude.com/docs/en/plugins/host-marketplace#turn-on-auto-update";

/** Note copy: plain text, or inline code with the text shown when it has no value. */
export type ClaudeCodeNotePart = string | { code: string; fallback: string };

const inlineCode = (code: string) => ({ code, fallback: code });

/**
 * Claude Code applies autoUpdate only from the extraKnownMarketplaces entry
 * keyed by the marketplace.json name, and installs enabledPlugins entries only
 * under that same name. The install notes and the setup steps share this copy
 * so the wording cannot drift.
 */
export function claudeMarketplaceNameNoteParts(marketplaceName: {
  code: string;
  fallback: string;
}): ClaudeCodeNotePart[] {
  return [
    "The ",
    inlineCode("extraKnownMarketplaces"),
    " key and the ",
    inlineCode("@"),
    " suffix in ",
    inlineCode("enabledPlugins"),
    " must be exactly ",
    marketplaceName,
    ", not the GitHub repository name. Otherwise Claude Code ignores ",
    inlineCode("autoUpdate"),
    " or never installs the plugin.",
  ];
}
