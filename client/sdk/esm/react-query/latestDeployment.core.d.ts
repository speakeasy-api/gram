import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetLatestDeploymentResult } from "../models/components/getlatestdeploymentresult.js";
import { GetLatestDeploymentRequest, GetLatestDeploymentSecurity } from "../models/operations/getlatestdeployment.js";
export type LatestDeploymentQueryData = GetLatestDeploymentResult;
export declare function prefetchLatestDeployment(queryClient: QueryClient, client$: GramCore, request?: GetLatestDeploymentRequest | undefined, security?: GetLatestDeploymentSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildLatestDeploymentQuery(client$: GramCore, request?: GetLatestDeploymentRequest | undefined, security?: GetLatestDeploymentSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<LatestDeploymentQueryData>;
};
export declare function queryKeyLatestDeployment(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=latestDeployment.core.d.ts.map