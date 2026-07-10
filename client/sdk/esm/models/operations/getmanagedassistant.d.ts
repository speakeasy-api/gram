import * as z from "zod/v4-mini";
export type GetManagedAssistantSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type GetManagedAssistantRequest = {
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
export type GetManagedAssistantSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetManagedAssistantSecurity$outboundSchema: z.ZodMiniType<GetManagedAssistantSecurity$Outbound, GetManagedAssistantSecurity>;
export declare function getManagedAssistantSecurityToJSON(getManagedAssistantSecurity: GetManagedAssistantSecurity): string;
/** @internal */
export type GetManagedAssistantRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetManagedAssistantRequest$outboundSchema: z.ZodMiniType<GetManagedAssistantRequest$Outbound, GetManagedAssistantRequest>;
export declare function getManagedAssistantRequestToJSON(getManagedAssistantRequest: GetManagedAssistantRequest): string;
//# sourceMappingURL=getmanagedassistant.d.ts.map