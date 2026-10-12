import type { WorkloadPlatform } from "@gram/client/models/components/workloadplatform.js";
import { WorkloadPlatformCatalog$inboundSchema } from "@gram/client/models/components/workloadplatformcatalog.js";
import catalogResponse from "./catalog.gen.json";

/**
 * The server's catalog, exactly as `workloadIdentities.listPlatforms` returns
 * it, for tests. `catalog.gen.json` is written from the catalog's YAML by
 * `mise run gen:workload-catalog-fixture`, and is parsed here the way the SDK
 * parses the response, so tests render the real entries.
 */
const catalogPlatforms: WorkloadPlatform[] =
  WorkloadPlatformCatalog$inboundSchema.parse(catalogResponse).platforms;

/** The Claude Tag entry, the catalog's first platform. */
export const claudeTagPlatform: WorkloadPlatform = (() => {
  const platform = catalogPlatforms.find((p) => p.key === "claude-tag");
  if (platform === undefined) {
    throw new Error("the generated catalog has no claude-tag entry");
  }
  return platform;
})();
