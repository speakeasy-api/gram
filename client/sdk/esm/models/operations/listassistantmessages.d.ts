import * as z from "zod/v4-mini";
export type ListAssistantMessagesSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListAssistantMessagesRequest = {
    /**
     * The chat id returned by sendMessage.
     */
    chatId: string;
    /**
     * Return only messages with seq greater than this; omit or 0 for the full log.
     */
    afterSeq?: number | undefined;
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
export type ListAssistantMessagesSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAssistantMessagesSecurity$outboundSchema: z.ZodMiniType<ListAssistantMessagesSecurity$Outbound, ListAssistantMessagesSecurity>;
export declare function listAssistantMessagesSecurityToJSON(listAssistantMessagesSecurity: ListAssistantMessagesSecurity): string;
/** @internal */
export type ListAssistantMessagesRequest$Outbound = {
    chat_id: string;
    after_seq?: number | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListAssistantMessagesRequest$outboundSchema: z.ZodMiniType<ListAssistantMessagesRequest$Outbound, ListAssistantMessagesRequest>;
export declare function listAssistantMessagesRequestToJSON(listAssistantMessagesRequest: ListAssistantMessagesRequest): string;
//# sourceMappingURL=listassistantmessages.d.ts.map