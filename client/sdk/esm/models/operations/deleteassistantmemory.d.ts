import * as z from "zod/v4-mini";
export type DeleteAssistantMemorySecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteAssistantMemoryRequest = {
    /**
     * The assistant memory ID.
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
export type DeleteAssistantMemorySecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteAssistantMemorySecurity$outboundSchema: z.ZodMiniType<DeleteAssistantMemorySecurity$Outbound, DeleteAssistantMemorySecurity>;
export declare function deleteAssistantMemorySecurityToJSON(deleteAssistantMemorySecurity: DeleteAssistantMemorySecurity): string;
/** @internal */
export type DeleteAssistantMemoryRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteAssistantMemoryRequest$outboundSchema: z.ZodMiniType<DeleteAssistantMemoryRequest$Outbound, DeleteAssistantMemoryRequest>;
export declare function deleteAssistantMemoryRequestToJSON(deleteAssistantMemoryRequest: DeleteAssistantMemoryRequest): string;
//# sourceMappingURL=deleteassistantmemory.d.ts.map