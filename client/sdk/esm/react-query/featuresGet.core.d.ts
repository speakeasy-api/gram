import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type FeaturesGetQueryData = components.GramProductFeatures;
export declare function prefetchFeaturesGet(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.GetProductFeaturesRequest | undefined,
  security?: operations.GetProductFeaturesSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildFeaturesGetQuery(
  client$: GramCore,
  request?: operations.GetProductFeaturesRequest | undefined,
  security?: operations.GetProductFeaturesSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<FeaturesGetQueryData>;
};
export declare function queryKeyFeaturesGet(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=featuresGet.core.d.ts.map
