import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListDeploymentResult } from "../models/components/listdeploymentresult.js";
import { ListDeploymentsRequest, ListDeploymentsSecurity } from "../models/operations/listdeployments.js";
export type ListDeploymentsQueryData = ListDeploymentResult;
export declare function prefetchListDeployments(queryClient: QueryClient, client$: GramCore, request?: ListDeploymentsRequest | undefined, security?: ListDeploymentsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListDeploymentsQuery(client$: GramCore, request?: ListDeploymentsRequest | undefined, security?: ListDeploymentsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListDeploymentsQueryData>;
};
export declare function queryKeyListDeployments(parameters: {
    cursor?: string | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listDeployments.core.d.ts.map