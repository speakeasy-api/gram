import { Gram } from "@gram/client";

export async function downloadPluginPackage(
  client: Gram,
  pluginId: string,
  platform: "claude" | "cursor" | "codex",
): Promise<void> {
  const { headers, result } = await client.plugins.downloadPluginPackage({
    pluginId,
    platform,
  });
  const blob = await new Response(result).blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download =
    headers["content-disposition"]?.[0]?.match(/filename="(.+)"/)?.[1] ??
    "plugin.zip";
  a.click();
  URL.revokeObjectURL(url);
}
