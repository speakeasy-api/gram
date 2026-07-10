import * as z from "zod/v4-mini";
export type ListEnvironmentsSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListEnvironmentsRequest = {
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
export type ListEnvironmentsSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListEnvironmentsSecurity$outboundSchema: z.ZodMiniType<ListEnvironmentsSecurity$Outbound, ListEnvironmentsSecurity>;
export declare function listEnvironmentsSecurityToJSON(listEnvironmentsSecurity: ListEnvironmentsSecurity): string;
/** @internal */
export type ListEnvironmentsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListEnvironmentsRequest$outboundSchema: z.ZodMiniType<ListEnvironmentsRequest$Outbound, ListEnvironmentsRequest>;
export declare function listEnvironmentsRequestToJSON(listEnvironmentsRequest: ListEnvironmentsRequest): string;
//# sourceMappingURL=listenvironments.d.ts.map