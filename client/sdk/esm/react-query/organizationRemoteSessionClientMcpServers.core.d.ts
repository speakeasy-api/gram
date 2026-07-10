import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListOrganizationMcpServersResult } from "../models/components/listorganizationmcpserversresult.js";
import { ListOrganizationRemoteSessionClientMcpServersRequest, ListOrganizationRemoteSessionClientMcpServersSecurity } from "../models/operations/listorganizationremotesessionclientmcpservers.js";
export type OrganizationRemoteSessionClientMcpServersQueryData = ListOrganizationMcpServersResult;
export declare function prefetchOrganizationRemoteSessionClientMcpServers(queryClient: QueryClient, client$: GramCore, request: ListOrganizationRemoteSessionClientMcpServersRequest, security?: ListOrganizationRemoteSessionClientMcpServersSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildOrganizationRemoteSessionClientMcpServersQuery(client$: GramCore, request: ListOrganizationRemoteSessionClientMcpServersRequest, security?: ListOrganizationRemoteSessionClientMcpServersSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<OrganizationRemoteSessionClientMcpServersQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionClientMcpServers(parameters: {
    clientId: string;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionClientMcpServers.core.d.ts.map