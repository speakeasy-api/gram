import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import { GetOrganizationRemoteSessionIssuerRequest, GetOrganizationRemoteSessionIssuerSecurity } from "../models/operations/getorganizationremotesessionissuer.js";
export type OrganizationRemoteSessionIssuerQueryData = RemoteSessionIssuer;
export declare function prefetchOrganizationRemoteSessionIssuer(queryClient: QueryClient, client$: GramCore, request: GetOrganizationRemoteSessionIssuerRequest, security?: GetOrganizationRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildOrganizationRemoteSessionIssuerQuery(client$: GramCore, request: GetOrganizationRemoteSessionIssuerRequest, security?: GetOrganizationRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<OrganizationRemoteSessionIssuerQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionIssuer(parameters: {
    id: string;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionIssuer.core.d.ts.map