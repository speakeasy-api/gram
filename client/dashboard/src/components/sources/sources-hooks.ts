import { useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import { useLatestDeployment } from "@gram/client/react-query/index.js";
import { useMemo } from "react";

export function useDeploymentIsEmpty() {
  const { data: deploymentResult, isLoading } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  if (isLoading) {
    return false;
  }

  return (
    !deployment ||
    (deployment.openapiv3Assets.length === 0 &&
      (deployment.functionsAssets?.length ?? 0) === 0 &&
      deployment.packages.length === 0 &&
      (deployment.externalMcps?.length ?? 0) === 0)
  );
}

export const useCatalogIconMap = () => {
  const { data: catalogData } = useInfiniteListMCPCatalog();
  return useMemo(() => {
    if (!catalogData?.pages) {
      return new Map<string, string>();
    }
    return new Map(
      catalogData.pages.flatMap((page) =>
        page.servers.map((s) => [s.registrySpecifier, s.iconUrl!]),
      ),
    );
  }, [catalogData]);
};
