import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListShadowMCPApprovalRequestsResult } from "../models/components/listshadowmcpapprovalrequestsresult.js";
import { ListShadowMCPApprovalRequestsRequest, ListShadowMCPApprovalRequestsSecurity, Status } from "../models/operations/listshadowmcpapprovalrequests.js";
export type ShadowMCPApprovalRequestsQueryData = ListShadowMCPApprovalRequestsResult;
export declare function prefetchShadowMCPApprovalRequests(queryClient: QueryClient, client$: GramCore, request?: ListShadowMCPApprovalRequestsRequest | undefined, security?: ListShadowMCPApprovalRequestsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildShadowMCPApprovalRequestsQuery(client$: GramCore, request?: ListShadowMCPApprovalRequestsRequest | undefined, security?: ListShadowMCPApprovalRequestsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ShadowMCPApprovalRequestsQueryData>;
};
export declare function queryKeyShadowMCPApprovalRequests(parameters: {
    status?: Status | undefined;
    projectId?: string | undefined;
    limit?: number | undefined;
    cursor?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=shadowMCPApprovalRequests.core.d.ts.map