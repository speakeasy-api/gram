import { useLatestDeployment } from "@/hooks/toolTypes";
import { useMemo } from "react";

/**
 * The sources a project has.
 *
 * A "source" is an OpenAPI document or a function in the latest deployment —
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
};

/** The asset id a source is keyed by, which is what its panel refetches from. */
export function sourceAssetId(source: SourceOption): string {
  return source.documentId ?? source.functionId ?? "";
}

export function useProjectSources(): {
  sources: SourceOption[];
  isLoading: boolean;
  isError: boolean;
} {
  const { data: deploymentResult, isLoading, isError } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const sources = useMemo(() => {
    const openapi = (deployment?.openapiv3Assets ?? []).map((asset) => ({
      key: `openapi:${asset.id}`,
      name: asset.name,
      kind: "openapi" as const,
      documentId: asset.id,
    }));
    const functions = (deployment?.functionsAssets ?? []).map((asset) => ({
      key: `function:${asset.id}`,
      name: asset.name,
      kind: "function" as const,
      functionId: asset.id,
    }));
    return [...openapi, ...functions];
  }, [deployment]);

  return { sources, isLoading, isError };
}
