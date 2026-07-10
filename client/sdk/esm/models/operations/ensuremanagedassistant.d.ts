import * as z from "zod/v4-mini";
export type EnsureManagedAssistantSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type EnsureManagedAssistantRequest = {
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
export type EnsureManagedAssistantSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const EnsureManagedAssistantSecurity$outboundSchema: z.ZodMiniType<EnsureManagedAssistantSecurity$Outbound, EnsureManagedAssistantSecurity>;
export declare function ensureManagedAssistantSecurityToJSON(ensureManagedAssistantSecurity: EnsureManagedAssistantSecurity): string;
/** @internal */
export type EnsureManagedAssistantRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const EnsureManagedAssistantRequest$outboundSchema: z.ZodMiniType<EnsureManagedAssistantRequest$Outbound, EnsureManagedAssistantRequest>;
export declare function ensureManagedAssistantRequestToJSON(ensureManagedAssistantRequest: EnsureManagedAssistantRequest): string;
//# sourceMappingURL=ensuremanagedassistant.d.ts.map