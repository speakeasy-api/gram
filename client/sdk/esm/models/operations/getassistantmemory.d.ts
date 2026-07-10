import * as z from "zod/v4-mini";
export type GetAssistantMemorySecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type GetAssistantMemoryRequest = {
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
export type GetAssistantMemorySecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetAssistantMemorySecurity$outboundSchema: z.ZodMiniType<GetAssistantMemorySecurity$Outbound, GetAssistantMemorySecurity>;
export declare function getAssistantMemorySecurityToJSON(getAssistantMemorySecurity: GetAssistantMemorySecurity): string;
/** @internal */
export type GetAssistantMemoryRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetAssistantMemoryRequest$outboundSchema: z.ZodMiniType<GetAssistantMemoryRequest$Outbound, GetAssistantMemoryRequest>;
export declare function getAssistantMemoryRequestToJSON(getAssistantMemoryRequest: GetAssistantMemoryRequest): string;
//# sourceMappingURL=getassistantmemory.d.ts.map