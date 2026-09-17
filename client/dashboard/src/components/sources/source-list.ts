import { useActiveDeployment } from "@/hooks/toolTypes";
import type { Asset } from "@gram/client/models/components/asset.js";
import { useListAssets } from "@gram/client/react-query/listAssets.js";
import { useMemo } from "react";

/**
 * The sources a project has.
 *
 * A "source" is an OpenAPI document or a function in the active deployment —
 * the two kinds that still produce tools. Sources have no pages of their own
 * beyond the shelf under MCP, so this is the whole of how they are listed.
 */
export type SourceOption = {
  key: string;
  name: string;
  kind: "openapi" | "function";
  /** Tools are matched to their source by these ids. */
  documentId?: string;
  functionId?: string;
  /**
   * The deployment's url-friendly label for the source. Stable across
   * versions, unlike the ids above, so it is what the CLI, the upload flow
   * and tool URNs address a source by.
   */
  slug?: string;
  /** The uploaded file behind the source, and what its serve endpoint takes. */
  assetId?: string;
  contentType?: string;
  createdAt?: Date;
  updatedAt?: Date;
};

/** The asset id a source is keyed by, which is what its panel refetches from. */
export function sourceAssetId(source: SourceOption): string {
  return source.documentId ?? source.functionId ?? "";
}

// The deployment names a source; the asset carries the file's own facts.
function fileFacts(
  file: Asset | undefined,
): Pick<SourceOption, "contentType" | "createdAt" | "updatedAt"> {
  if (!file) return {};
  return {
    contentType: file.contentType,
    createdAt: file.createdAt,
    updatedAt: file.updatedAt,
  };
}

export function useProjectSources(): {
  sources: SourceOption[];
  isLoading: boolean;
  isError: boolean;
  /** The file facts (format, dates) have not arrived yet. */
  assetsLoading: boolean;
  /**
   * The file facts could not be read. Sources still list, but anything
   * keyed on those facts should say they are unknown rather than treat them
   * as absent.
   */
  assetsUnavailable: boolean;
} {
  const { data: deploymentResult, isLoading, isError } = useActiveDeployment();
  // Loading is keyed to the deployment alone: the assets only add dates and a
  // format, and the list is usable before they land — or without them, so a
  // failed read is not thrown to the page's error boundary.
  const {
    data: assetsResult,
    isLoading: assetsLoading,
    isError: assetsError,
  } = useListAssets(undefined, undefined, { throwOnError: false });
  const deployment = deploymentResult?.deployment;

  const sources = useMemo(() => {
    const filesById = new Map(
      (assetsResult?.assets ?? []).map((file) => [file.id, file]),
    );
    const openapi = (deployment?.openapiv3Assets ?? []).map((asset) => ({
      key: `openapi:${asset.id}`,
      name: asset.name,
      kind: "openapi" as const,
      documentId: asset.id,
      slug: asset.slug,
      assetId: asset.assetId,
      ...fileFacts(filesById.get(asset.assetId)),
    }));
    const functions = (deployment?.functionsAssets ?? []).map((asset) => ({
      key: `function:${asset.id}`,
      name: asset.name,
      kind: "function" as const,
      functionId: asset.id,
      slug: asset.slug,
      assetId: asset.assetId,
      ...fileFacts(filesById.get(asset.assetId)),
    }));
    return [...openapi, ...functions];
  }, [deployment, assetsResult]);

  return {
    sources,
    isLoading,
    isError,
    assetsLoading,
    assetsUnavailable: assetsError && !assetsResult,
  };
}
