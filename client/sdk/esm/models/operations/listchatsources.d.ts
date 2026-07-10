import * as z from "zod/v4-mini";
export type ListChatSourcesSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListChatSourcesSecurityOption2 = {
    chatSessionsTokenHeaderGramChatSession: string;
};
export type ListChatSourcesSecurity = {
    option1?: ListChatSourcesSecurityOption1 | undefined;
    option2?: ListChatSourcesSecurityOption2 | undefined;
};
export type ListChatSourcesRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    /**
     * Chat Sessions token header
     */
    gramChatSession?: string | undefined;
};
/** @internal */
export type ListChatSourcesSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListChatSourcesSecurityOption1$outboundSchema: z.ZodMiniType<ListChatSourcesSecurityOption1$Outbound, ListChatSourcesSecurityOption1>;
export declare function listChatSourcesSecurityOption1ToJSON(listChatSourcesSecurityOption1: ListChatSourcesSecurityOption1): string;
/** @internal */
export type ListChatSourcesSecurityOption2$Outbound = {
    "chat_sessions_token_header_Gram-Chat-Session": string;
};
/** @internal */
export declare const ListChatSourcesSecurityOption2$outboundSchema: z.ZodMiniType<ListChatSourcesSecurityOption2$Outbound, ListChatSourcesSecurityOption2>;
export declare function listChatSourcesSecurityOption2ToJSON(listChatSourcesSecurityOption2: ListChatSourcesSecurityOption2): string;
/** @internal */
export type ListChatSourcesSecurity$Outbound = {
    Option1?: ListChatSourcesSecurityOption1$Outbound | undefined;
    Option2?: ListChatSourcesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListChatSourcesSecurity$outboundSchema: z.ZodMiniType<ListChatSourcesSecurity$Outbound, ListChatSourcesSecurity>;
export declare function listChatSourcesSecurityToJSON(listChatSourcesSecurity: ListChatSourcesSecurity): string;
/** @internal */
export type ListChatSourcesRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Gram-Chat-Session"?: string | undefined;
};
/** @internal */
export declare const ListChatSourcesRequest$outboundSchema: z.ZodMiniType<ListChatSourcesRequest$Outbound, ListChatSourcesRequest>;
export declare function listChatSourcesRequestToJSON(listChatSourcesRequest: ListChatSourcesRequest): string;
//# sourceMappingURL=listchatsources.d.ts.map