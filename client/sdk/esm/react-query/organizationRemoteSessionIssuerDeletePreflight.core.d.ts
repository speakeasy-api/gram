import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { OrganizationIssuerDeletePreflight } from "../models/components/organizationissuerdeletepreflight.js";
import { GetOrganizationRemoteSessionIssuerDeletePreflightRequest, GetOrganizationRemoteSessionIssuerDeletePreflightSecurity } from "../models/operations/getorganizationremotesessionissuerdeletepreflight.js";
export type OrganizationRemoteSessionIssuerDeletePreflightQueryData = OrganizationIssuerDeletePreflight;
export declare function prefetchOrganizationRemoteSessionIssuerDeletePreflight(queryClient: QueryClient, client$: GramCore, request: GetOrganizationRemoteSessionIssuerDeletePreflightRequest, security?: GetOrganizationRemoteSessionIssuerDeletePreflightSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildOrganizationRemoteSessionIssuerDeletePreflightQuery(client$: GramCore, request: GetOrganizationRemoteSessionIssuerDeletePreflightRequest, security?: GetOrganizationRemoteSessionIssuerDeletePreflightSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<OrganizationRemoteSessionIssuerDeletePreflightQueryData>;
};
export declare function queryKeyOrganizationRemoteSessionIssuerDeletePreflight(parameters: {
    id: string;
    gramSession?: string | undefined;
    gramKey?: string | undefined;
}): QueryKey;
//# sourceMappingURL=organizationRemoteSessionIssuerDeletePreflight.core.d.ts.map