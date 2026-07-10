import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetProductFeaturesResponseBody } from "../models/components/getproductfeaturesresponsebody.js";
import { GetProductFeaturesRequest, GetProductFeaturesSecurity } from "../models/operations/getproductfeatures.js";
export type ProductFeaturesQueryData = GetProductFeaturesResponseBody;
export declare function prefetchProductFeatures(queryClient: QueryClient, client$: GramCore, request?: GetProductFeaturesRequest | undefined, security?: GetProductFeaturesSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildProductFeaturesQuery(client$: GramCore, request?: GetProductFeaturesRequest | undefined, security?: GetProductFeaturesSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ProductFeaturesQueryData>;
};
export declare function queryKeyProductFeatures(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=productFeatures.core.d.ts.map