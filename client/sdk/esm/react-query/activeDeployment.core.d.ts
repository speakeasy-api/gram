import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetActiveDeploymentResult } from "../models/components/getactivedeploymentresult.js";
import { GetActiveDeploymentRequest, GetActiveDeploymentSecurity } from "../models/operations/getactivedeployment.js";
export type ActiveDeploymentQueryData = GetActiveDeploymentResult;
export declare function prefetchActiveDeployment(queryClient: QueryClient, client$: GramCore, request?: GetActiveDeploymentRequest | undefined, security?: GetActiveDeploymentSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildActiveDeploymentQuery(client$: GramCore, request?: GetActiveDeploymentRequest | undefined, security?: GetActiveDeploymentSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ActiveDeploymentQueryData>;
};
export declare function queryKeyActiveDeployment(parameters: {
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=activeDeployment.core.d.ts.map