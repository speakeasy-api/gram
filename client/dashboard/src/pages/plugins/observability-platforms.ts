import type { Platform } from "@gram/client/models/operations/downloadobservabilityplugin.js";

/**
 * The platforms `plugins.downloadObservabilityPlugin` can package the
 * observability plugin for. Kept in one place so the plugin card's download menu
 * and the credential-rotation dialog cannot drift apart. The `platform` values
 * are typed against the SDK's `Platform` enum so a design-side enum change
 * breaks the build here instead of silently dropping a platform.
 */
export const OBSERVABILITY_DOWNLOAD_PLATFORMS: {
  platform: Platform;
  label: string;
}[] = [
  { platform: "claude", label: "Claude" },
  { platform: "cursor", label: "Cursor" },
  { platform: "codex", label: "Codex" },
  { platform: "opencode", label: "OpenCode" },
  { platform: "copilot", label: "Copilot CLI" },
  { platform: "openclaw", label: "OpenClaw" },
  { platform: "pi", label: "Pi" },
  { platform: "vibe", label: "Mistral Vibe" },
];

export type ObservabilityDownloadPlatform =
  (typeof OBSERVABILITY_DOWNLOAD_PLATFORMS)[number]["platform"];
