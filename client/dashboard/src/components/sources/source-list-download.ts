import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useCallback } from "react";
import { toast } from "sonner";
import type { SourceOption } from "./source-list";
import { downloadSourceFile, sourceDownloadFilename } from "./sourceDownload";

/**
 * Returns a callback that downloads a source's file, toasting on failure.
 *
 * A callback rather than a button, so it can sit in a row's action menu.
 */
export function useDownloadSource(): (source: SourceOption) => Promise<void> {
  const project = useProject();
  const { projectSlug } = useSlugs();

  return useCallback(
    async (source: SourceOption) => {
      if (!source.assetId) {
        toast.error("This source has no file to download yet.");
        return;
      }
      try {
        await downloadSourceFile({
          assetId: source.assetId,
          projectId: project.id,
          projectSlug,
          isOpenAPI: source.kind === "openapi",
          filename: (blob) => sourceDownloadFilename(source, blob),
        });
      } catch (error) {
        toast.error(
          error instanceof Error
            ? `Couldn't download this source: ${error.message}`
            : "Couldn't download this source",
        );
      }
    },
    [project.id, projectSlug],
  );
}
