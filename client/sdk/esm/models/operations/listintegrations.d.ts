import * as z from "zod/v4-mini";
export type ListIntegrationsSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListIntegrationsRequest = {
    /**
     * Keywords to filter integrations by
     */
    keywords?: Array<string> | undefined;
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
export type ListIntegrationsSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListIntegrationsSecurity$outboundSchema: z.ZodMiniType<ListIntegrationsSecurity$Outbound, ListIntegrationsSecurity>;
export declare function listIntegrationsSecurityToJSON(listIntegrationsSecurity: ListIntegrationsSecurity): string;
/** @internal */
export type ListIntegrationsRequest$Outbound = {
    keywords?: Array<string> | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListIntegrationsRequest$outboundSchema: z.ZodMiniType<ListIntegrationsRequest$Outbound, ListIntegrationsRequest>;
export declare function listIntegrationsRequestToJSON(listIntegrationsRequest: ListIntegrationsRequest): string;
//# sourceMappingURL=listintegrations.d.ts.map