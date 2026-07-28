export type PluginDownloadPlatform = "claude" | "cursor" | "codex" | "opencode";

// Downloads over the raw endpoint rather than the generated SDK client: the
// SDK pins the platform enum from its last generation, which would reject a
// platform the server already accepts until the SDK is regenerated.
export async function downloadPluginPackage(
  authFetch: (endpoint: string, opts: RequestInit) => Promise<Response>,
  pluginId: string,
  platform: PluginDownloadPlatform,
): Promise<void> {
  const resp = await authFetch(
    `/rpc/plugins.downloadPluginPackage?plugin_id=${encodeURIComponent(pluginId)}&platform=${platform}`,
    {},
  );
  if (!resp.ok) {
    throw new Error(`download plugin package: ${resp.status}`);
  }
  const blob = await resp.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download =
    // Non-greedy so a header with additional quoted params (e.g.
    // `filename="x.zip"; creation-date="..."`) doesn't overcapture.
    resp.headers.get("Content-Disposition")?.match(/filename="(.+?)"/)?.[1] ??
    "plugin.zip";
  a.click();
  URL.revokeObjectURL(url);
}
