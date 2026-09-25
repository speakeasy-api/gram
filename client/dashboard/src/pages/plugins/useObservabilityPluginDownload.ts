import { useFetcher } from "@/contexts/Fetcher";
import { useState } from "react";
import { toast } from "sonner";

/**
 * Downloads the server-generated observability plugin ZIP. Platforms
 * differ only in the `platform` query value and the fallback filename.
 */
export function useObservabilityPluginDownload(
  platform: string,
  fallbackName: string,
): { isDownloading: boolean; download: () => Promise<void> } {
  const { fetch: authFetch } = useFetcher();
  const [isDownloading, setIsDownloading] = useState(false);

  const download = async () => {
    setIsDownloading(true);
    try {
      const resp = await authFetch(
        `/rpc/plugins.downloadObservabilityPlugin?platform=${platform}`,
        {},
      );
      if (!resp.ok) {
        toast.error(
          resp.status === 403
            ? "Downloading the observability plugin requires an org admin."
            : "Failed to download observability plugin",
        );
        return;
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download =
        resp.headers
          .get("Content-Disposition")
          ?.match(/filename="(.+)"/)?.[1] ?? fallbackName;
      a.click();
      // Revoke on the next task: some browsers kick the blob download off
      // asynchronously and a same-task revoke aborts it.
      setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch (err) {
      toast.error("Failed to download observability plugin");
      console.error("observability plugin download failed", err);
    } finally {
      setIsDownloading(false);
    }
  };

  return { isDownloading, download };
}
