import { useRef, useState } from "react";

import type { DownloadPluginPackageQueryParamPlatform } from "@gram/client/models/operations/downloadpluginpackage.js";
import { Gram } from "@gram/client";
import { toast } from "sonner";

export type PluginPackagePlatform = DownloadPluginPackageQueryParamPlatform;

// The SDK returns headers as a plain Record<string, string[]>, not a Fetch
// Headers instance, so lookups must be done case-insensitively by hand — the
// server's casing isn't guaranteed to match the lowercase key we'd otherwise
// index with directly.
function getHeader(
  headers: Record<string, string[]>,
  name: string,
): string | undefined {
  const key = Object.keys(headers).find(
    (k) => k.toLowerCase() === name.toLowerCase(),
  );
  return key ? headers[key]?.[0] : undefined;
}

export async function downloadPluginPackage(
  client: Gram,
  pluginId: string,
  platform: PluginPackagePlatform,
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
    // Non-greedy so a header with additional quoted params (e.g.
    // `filename="x.zip"; creation-date="..."`) doesn't overcapture.
    getHeader(headers, "Content-Disposition")?.match(/filename="(.+?)"/)?.[1] ??
    "plugin.zip";
  a.click();
  URL.revokeObjectURL(url);
}

export function usePluginPackageDownload(
  client: Gram,
  pluginId: string,
  onMenuOpenChange: (open: boolean) => void,
): {
  isDownloading: boolean;
  download: (platform: PluginPackagePlatform) => Promise<void>;
} {
  const [downloadingPluginId, setDownloadingPluginId] = useState<string | null>(
    null,
  );
  const activeRequestRef = useRef<{
    pluginId: string;
    request: symbol;
  } | null>(null);

  const download = async (platform: PluginPackagePlatform): Promise<void> => {
    if (activeRequestRef.current?.pluginId === pluginId) return;
    const request = Symbol();
    activeRequestRef.current = { pluginId, request };
    onMenuOpenChange(false);
    setDownloadingPluginId(pluginId);
    try {
      await downloadPluginPackage(client, pluginId, platform);
    } catch (_err) {
      toast.error("Failed to download plugin package");
    } finally {
      if (activeRequestRef.current?.request === request) {
        activeRequestRef.current = null;
        setDownloadingPluginId(null);
      }
    }
  };

  return { isDownloading: downloadingPluginId === pluginId, download };
}
