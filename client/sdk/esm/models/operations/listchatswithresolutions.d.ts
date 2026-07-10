import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListChatsWithResolutionsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListChatsWithResolutionsSecurityOption2 = {
    chatSessionsTokenHeaderGramChatSession: string;
};
export type ListChatsWithResolutionsSecurity = {
    option1?: ListChatsWithResolutionsSecurityOption1 | undefined;
    option2?: ListChatsWithResolutionsSecurityOption2 | undefined;
};
/**
 * Filter by whether chat has risk findings: 'true', 'false', or empty for no filter.
 */
export declare const HasRisk: {
    readonly Unknown: "";
    readonly True: "true";
    readonly False: "false";
};
/**
 * Filter by whether chat has risk findings: 'true', 'false', or empty for no filter.
 */
export type HasRisk = ClosedEnum<typeof HasRisk>;
/**
 * Field to sort by
 */
export declare const SortBy: {
    readonly CreatedAt: "created_at";
    readonly NumMessages: "num_messages";
    readonly Score: "score";
};
/**
 * Field to sort by
 */
export type SortBy = ClosedEnum<typeof SortBy>;
/**
 * Sort order
 */
export declare const SortOrder: {
    readonly Asc: "asc";
    readonly Desc: "desc";
};
/**
 * Sort order
 */
export type SortOrder = ClosedEnum<typeof SortOrder>;
export type ListChatsWithResolutionsRequest = {
    /**
     * Search query (searches chat ID, user ID, and title)
     */
    search?: string | undefined;
    /**
     * Filter by external user ID
     */
    externalUserId?: string | undefined;
    /**
     * Filter to chats produced by this assistant
     */
    assistantId?: string | undefined;
    /**
     * Filter by resolution status
     */
    resolutionStatus?: string | undefined;
    /**
     * Filter by whether chat has risk findings: 'true', 'false', or empty for no filter.
     */
    hasRisk?: HasRisk | undefined;
    /**
     * Filter chats created after this timestamp (ISO 8601)
     */
    from?: Date | undefined;
    /**
     * Filter chats created before this timestamp (ISO 8601)
     */
    to?: Date | undefined;
    /**
     * Number of results per page
     */
    limit?: number | undefined;
    /**
     * Pagination offset
     */
    offset?: number | undefined;
    /**
     * Field to sort by
     */
    sortBy?: SortBy | undefined;
    /**
     * Sort order
     */
    sortOrder?: SortOrder | undefined;
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
export type ListChatsWithResolutionsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListChatsWithResolutionsSecurityOption1$outboundSchema: z.ZodMiniType<ListChatsWithResolutionsSecurityOption1$Outbound, ListChatsWithResolutionsSecurityOption1>;
export declare function listChatsWithResolutionsSecurityOption1ToJSON(listChatsWithResolutionsSecurityOption1: ListChatsWithResolutionsSecurityOption1): string;
/** @internal */
export type ListChatsWithResolutionsSecurityOption2$Outbound = {
    "chat_sessions_token_header_Gram-Chat-Session": string;
};
/** @internal */
export declare const ListChatsWithResolutionsSecurityOption2$outboundSchema: z.ZodMiniType<ListChatsWithResolutionsSecurityOption2$Outbound, ListChatsWithResolutionsSecurityOption2>;
export declare function listChatsWithResolutionsSecurityOption2ToJSON(listChatsWithResolutionsSecurityOption2: ListChatsWithResolutionsSecurityOption2): string;
/** @internal */
export type ListChatsWithResolutionsSecurity$Outbound = {
    Option1?: ListChatsWithResolutionsSecurityOption1$Outbound | undefined;
    Option2?: ListChatsWithResolutionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListChatsWithResolutionsSecurity$outboundSchema: z.ZodMiniType<ListChatsWithResolutionsSecurity$Outbound, ListChatsWithResolutionsSecurity>;
export declare function listChatsWithResolutionsSecurityToJSON(listChatsWithResolutionsSecurity: ListChatsWithResolutionsSecurity): string;
/** @internal */
export declare const HasRisk$outboundSchema: z.ZodMiniEnum<typeof HasRisk>;
/** @internal */
export declare const SortBy$outboundSchema: z.ZodMiniEnum<typeof SortBy>;
/** @internal */
export declare const SortOrder$outboundSchema: z.ZodMiniEnum<typeof SortOrder>;
/** @internal */
export type ListChatsWithResolutionsRequest$Outbound = {
    search?: string | undefined;
    external_user_id?: string | undefined;
    assistant_id?: string | undefined;
    resolution_status?: string | undefined;
    has_risk?: string | undefined;
    from?: string | undefined;
    to?: string | undefined;
    limit: number;
    offset: number;
    sort_by: string;
    sort_order: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    "Gram-Chat-Session"?: string | undefined;
};
/** @internal */
export declare const ListChatsWithResolutionsRequest$outboundSchema: z.ZodMiniType<ListChatsWithResolutionsRequest$Outbound, ListChatsWithResolutionsRequest>;
export declare function listChatsWithResolutionsRequestToJSON(listChatsWithResolutionsRequest: ListChatsWithResolutionsRequest): string;
//# sourceMappingURL=listchatswithresolutions.d.ts.map