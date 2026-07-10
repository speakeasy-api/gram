import * as z from "zod/v4-mini";
export type ListProjectsSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListProjectsRequest = {
    /**
     * The ID of the organization to list projects for
     */
    organizationId: string;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type ListProjectsSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListProjectsSecurity$outboundSchema: z.ZodMiniType<ListProjectsSecurity$Outbound, ListProjectsSecurity>;
export declare function listProjectsSecurityToJSON(listProjectsSecurity: ListProjectsSecurity): string;
/** @internal */
export type ListProjectsRequest$Outbound = {
    organization_id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListProjectsRequest$outboundSchema: z.ZodMiniType<ListProjectsRequest$Outbound, ListProjectsRequest>;
export declare function listProjectsRequestToJSON(listProjectsRequest: ListProjectsRequest): string;
//# sourceMappingURL=listprojects.d.ts.map