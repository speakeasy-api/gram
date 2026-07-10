import * as z from "zod/v4-mini";
export type DeleteAssistantSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteAssistantRequest = {
    /**
     * The assistant ID.
     */
    id: string;
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
export type DeleteAssistantSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteAssistantSecurity$outboundSchema: z.ZodMiniType<DeleteAssistantSecurity$Outbound, DeleteAssistantSecurity>;
export declare function deleteAssistantSecurityToJSON(deleteAssistantSecurity: DeleteAssistantSecurity): string;
/** @internal */
export type DeleteAssistantRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteAssistantRequest$outboundSchema: z.ZodMiniType<DeleteAssistantRequest$Outbound, DeleteAssistantRequest>;
export declare function deleteAssistantRequestToJSON(deleteAssistantRequest: DeleteAssistantRequest): string;
//# sourceMappingURL=deleteassistant.d.ts.map