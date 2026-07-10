import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListOrganizationRemoteSessionIssuersRequest, ListOrganizationRemoteSessionIssuersResponse, ListOrganizationRemoteSessionIssuersSecurity } from "../models/operations/listorganizationremotesessionissuers.js";
import { PageIterator } from "../types/operations.js";
export type OrganizationRemoteSessionIssuersQueryData = ListOrganizationRemoteSessionIssuersResponse;
export type OrganizationRemoteSessionIssuersInfiniteQueryData = PageIterator<ListOrganizationRemoteSessionIssuersResponse, {
    cursor: string;
}>;
export type OrganizationRemoteSessionIssuersPageParams = PageIterator<ListOrganizationRemoteSessionIssuersResponse, {
    cursor: string;
}>["~next"];
export declare function prefetchOrganizationRemoteSessionIssuers(queryClient: QueryClient, client$: GramCore, request?: ListOrganizationRemoteSessionIssuersRequest | undefined, security?: ListOrganizationRemoteSessionIssuersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function prefetchOrganizationRemoteSessionIssuersInfinite(queryClient: QueryClient, client$: GramCore, request?: ListOrganizationRemoteSessionIssuersRequest | undefined, security?: ListOrganizationRemoteSessionIssuersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildOrganizationRemoteSessionIssuersQuery(client$: GramCore, request?: ListOrganizationRemoteSessionIssuersRequest | undefined, security?: ListOrganizationRemoteSessionIssuersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<OrganizationRemoteSessionIssuersQueryData>;
};
export declare function buildOrganizationRemoteSessionIssuersInfiniteQuery(client$: GramCore, request?: ListOrganizationRemoteSessionIssuersRequest | undefined, security?: ListOrganizationRemoteSessionIssuersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext<QueryKey, OrganizationRemoteSessionIssuersPageParams>) => Promise<OrganizationRemoteSessionIssuersInfiniteQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionIssuers(parameters: {
    cursor?: string | undefined;
    limit?: number | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
}): QueryKey;
export declare function queryKeyOrganizationRemoteSessionIssuersInfinite(parameters: {
    cursor?: string | undefined;
    limit?: number | undefined;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionIssuers.core.d.ts.map