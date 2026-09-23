import { useRef, useState } from "react";

import type {
  DownloadPluginPackageRequest,
  QueryParamPlatform,
} from "@gram/client/models/operations/downloadpluginpackage.js";
import { Gram } from "@gram/client";
import { toast } from "sonner";
import {
  downloadBlob,
  filenameFromContentDisposition,
  headerValue,
} from "@/lib/download";

export type PluginPackagePlatform = QueryParamPlatform;
type PluginPackageScope = Pick<
  DownloadPluginPackageRequest,
  "gramProject" | "gramSession"
>;

export async function downloadResponse(
  response: Response,
  fallbackFilename: string,
): Promise<void> {
  if (!response.ok)
    throw new Error(`download failed with status ${response.status}`);
  downloadBlob(
    await response.blob(),
    filenameFromContentDisposition(
      response.headers.get("Content-Disposition"),
      fallbackFilename,
    ),
  );
}

export async function downloadPluginPackage(
  client: Gram,
  pluginId: string,
  platform: PluginPackagePlatform,
  scope: PluginPackageScope,
): Promise<void> {
  const { headers, result } = await client.plugins.downloadPluginPackage({
    ...scope,
    pluginId,
    platform,
  });
  downloadBlob(
    await new Response(result).blob(),
    filenameFromContentDisposition(
      headerValue(headers, "Content-Disposition"),
      "plugin.zip",
    ),
  );
}

export function usePluginPackageDownload(
  client: Gram,
  pluginId: string,
  onMenuOpenChange: (open: boolean) => void,
  scope: PluginPackageScope,
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
      await downloadPluginPackage(client, pluginId, platform, scope);
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
