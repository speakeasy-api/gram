/**
 * The platforms `plugins.downloadObservabilityPlugin` can package the
 * observability plugin for. Kept in one place so the plugin card's download menu
 * and the credential-rotation dialog cannot drift apart.
 */
export const OBSERVABILITY_DOWNLOAD_PLATFORMS = [
  { platform: "claude", label: "Claude" },
  // Uploaded in Claude's admin plugin settings for Cowork: ships each
  // session's transcript instead of relaying per-event hooks.
  { platform: "claude-cowork", label: "Claude Cowork" },
  { platform: "cursor", label: "Cursor" },
  { platform: "codex", label: "Codex" },
  { platform: "opencode", label: "OpenCode" },
  { platform: "copilot", label: "Copilot CLI" },
  { platform: "openclaw", label: "OpenClaw" },
  { platform: "pi", label: "Pi" },
] as const;

export type ObservabilityDownloadPlatform =
  (typeof OBSERVABILITY_DOWNLOAD_PLATFORMS)[number]["platform"];
