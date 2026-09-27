import { downloadBlob } from "@/lib/download";
import { getServerURL } from "@/lib/utils";
import type { SourceKind } from "./sourceVersions";

/**
 * Fetch a source's file and hand it to the browser as a download.
 *
 * The serve endpoints stream the raw file rather than JSON, so they sit
 * outside the generated SDK: fetch them by hand and hand the blob to an
 * anchor. A plain <a download> can't do it — the request needs the session
 * cookie and the `gram-project` header. Shared by the page and panel button
 * and by the shelf's row action, so the endpoint and lifecycle live once.
 */
export async function downloadSourceFile({
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
  downloadBlob(blob, await filename(blob));
}

/**
 * The file the user gets back should be named like the thing they picked, and
 * carry the extension its content actually has: a function bundle is a zip,
 * and an OpenAPI document is whichever of YAML/JSON was uploaded.
 * When the file facts did not load there is no content type to go by, so the
 * document itself decides: JSON opens with a brace, YAML does not.
 */
export async function sourceDownloadFilename(
  source: { kind: SourceKind; name: string; contentType?: string | undefined },
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
