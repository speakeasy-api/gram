import * as z from "zod/v4-mini";
export type ListResourcesSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListResourcesRequest = {
    /**
     * The cursor to fetch results from
     */
    cursor?: string | undefined;
    /**
     * The number of resources to return per page
     */
    limit?: number | undefined;
    /**
     * The deployment ID. If unset, latest deployment will be used.
     */
    deploymentId?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type ListResourcesSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListResourcesSecurity$outboundSchema: z.ZodMiniType<ListResourcesSecurity$Outbound, ListResourcesSecurity>;
export declare function listResourcesSecurityToJSON(listResourcesSecurity: ListResourcesSecurity): string;
/** @internal */
export type ListResourcesRequest$Outbound = {
    cursor?: string | undefined;
    limit?: number | undefined;
    deployment_id?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListResourcesRequest$outboundSchema: z.ZodMiniType<ListResourcesRequest$Outbound, ListResourcesRequest>;
export declare function listResourcesRequestToJSON(listResourcesRequest: ListResourcesRequest): string;
//# sourceMappingURL=listresources.d.ts.map