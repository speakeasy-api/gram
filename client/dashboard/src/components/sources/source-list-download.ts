import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useCallback } from "react";
import { toast } from "sonner";
import type { SourceOption } from "./source-list";

// The serve endpoints stream the raw file rather than JSON, so they sit
// outside the generated SDK: fetch them by hand and hand the blob to an
// anchor. A plain <a download> can't do it — the request needs the session
// cookie and the `gram-project` header. Twin of the fetch in
// SourceDetailPanel's SourceDownloadButton, which is a button rather than a
// callback and so can't sit in a row's action menu.
async function downloadSourceFile({
  assetId,
  projectId,
  projectSlug,
  isOpenAPI,
  filename,
}: {
  assetId: string;
  projectId: string;
  projectSlug: string | undefined;
  isOpenAPI: boolean;
  /** Named once the file is in hand, so its content can settle the extension. */
  filename: (blob: Blob) => Promise<string>;
}): Promise<void> {
  const url = new URL(
    isOpenAPI ? "/rpc/assets.serveOpenAPIv3" : "/rpc/assets.serveFunction",
    getServerURL(),
  );
  url.searchParams.set("id", assetId);
  url.searchParams.set("project_id", projectId);

  const request = new Request(url.toString(), {
    method: "GET",
    credentials: "include",
  });
  if (projectSlug) request.headers.set("gram-project", projectSlug);

  const response = await fetch(request);
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }

  const blob = await response.blob();
  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = objectUrl;
  anchor.download = await filename(blob);
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // Revoked a task later: browsers that start the download asynchronously
  // read the URL after click() returns, and pulling it out from under them
  // saves an empty file.
  setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
}

// The file the user gets back should be named like the thing they picked, and
// carry the extension its content actually has: a function bundle is a zip,
// and an OpenAPI document is whichever of YAML/JSON was uploaded.
// When the file facts did not load there is no content type to go by, so the
// document itself decides: JSON opens with a brace, YAML does not.
async function sourceDownloadFilename(
  source: Pick<SourceOption, "kind" | "name" | "contentType">,
  blob: Blob,
): Promise<string> {
  const base = source.name.replace(/\.(zip|ya?ml|json)$/i, "") || "source";
  if (source.kind !== "openapi") return `${base}.zip`;
  if (source.contentType) {
    return `${base}.${source.contentType.includes("json") ? "json" : "yaml"}`;
  }
  const head = (await blob.slice(0, 64).text()).trimStart();
  return `${base}.${head.startsWith("{") ? "json" : "yaml"}`;
}

/** Returns a callback that downloads a source's file, toasting on failure. */
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
