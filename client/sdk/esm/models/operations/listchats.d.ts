import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListChatsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListChatsSecurityOption2 = {
    chatSessionsTokenHeaderGramChatSession: string;
};
export type ListChatsSecurity = {
    option1?: ListChatsSecurityOption1 | undefined;
    option2?: ListChatsSecurityOption2 | undefined;
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
 * Filter by AI account type: 'team', 'personal', or empty for no filter.
 */
export declare const AccountType: {
    readonly Unknown: "";
    readonly Team: "team";
    readonly Personal: "personal";
};
/**
 * Filter by AI account type: 'team', 'personal', or empty for no filter.
 */
export type AccountType = ClosedEnum<typeof AccountType>;
/**
 * Filter by pinned state: 'true' for pinned chats, 'false' for unpinned, or empty for no filter.
 */
export declare const Pinned: {
    readonly Unknown: "";
    readonly True: "true";
    readonly False: "false";
};
/**
 * Filter by pinned state: 'true' for pinned chats, 'false' for unpinned, or empty for no filter.
 */
export type Pinned = ClosedEnum<typeof Pinned>;
/**
 * Field to sort by
 */
export declare const SortBy: {
    readonly LastMessageTimestamp: "last_message_timestamp";
    readonly NumMessages: "num_messages";
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
export type ListChatsRequest = {
    /**
     * Search query (searches chat ID, user ID, and title)
     */
    search?: string | undefined;
    /**
     * Filter by external user ID
     */
    externalUserId?: string | undefined;
    /**
     * Filter by agent source. Comma-separated list of exact source values (e.g. 'claude-code,Codex,playground') matched against each session's inferred source; empty for no filter. Use chat.listSources to discover the available values.
     */
    source?: string | undefined;
    /**
     * Filter to chats produced by this assistant
     */
    assistantId?: string | undefined;
    /**
     * When set with assistant_id, list only that assistant's threads whose source_kind matches this value (e.g. 'setup' for onboarding threads). Empty for no filter.
     */
    sourceKind?: string | undefined;
    /**
     * When set with assistant_id, exclude that assistant's threads whose source_kind matches this value (e.g. 'setup' to hide onboarding threads from runtime views). Empty for no filter.
     */
    excludeSourceKind?: string | undefined;
    /**
     * Filter by whether chat has risk findings: 'true', 'false', or empty for no filter.
     */
    hasRisk?: HasRisk | undefined;
    /**
     * Filter by AI account type: 'team', 'personal', or empty for no filter.
     */
    accountType?: AccountType | undefined;
    /**
     * Filter by pinned state: 'true' for pinned chats, 'false' for unpinned, or empty for no filter.
     */
    pinned?: Pinned | undefined;
    /**
     * Filter to chats with at least this many active risk findings (inclusive). Omit or pass 0 for no threshold.
     */
    minRiskScore?: number | undefined;
    /**
     * Filter chats last active after this timestamp (ISO 8601)
     */
    from?: Date | undefined;
    /**
     * Filter chats last active before this timestamp (ISO 8601)
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
export type ListChatsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListChatsSecurityOption1$outboundSchema: z.ZodMiniType<ListChatsSecurityOption1$Outbound, ListChatsSecurityOption1>;
export declare function listChatsSecurityOption1ToJSON(listChatsSecurityOption1: ListChatsSecurityOption1): string;
/** @internal */
export type ListChatsSecurityOption2$Outbound = {
    "chat_sessions_token_header_Gram-Chat-Session": string;
};
/** @internal */
export declare const ListChatsSecurityOption2$outboundSchema: z.ZodMiniType<ListChatsSecurityOption2$Outbound, ListChatsSecurityOption2>;
export declare function listChatsSecurityOption2ToJSON(listChatsSecurityOption2: ListChatsSecurityOption2): string;
/** @internal */
export type ListChatsSecurity$Outbound = {
    Option1?: ListChatsSecurityOption1$Outbound | undefined;
    Option2?: ListChatsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListChatsSecurity$outboundSchema: z.ZodMiniType<ListChatsSecurity$Outbound, ListChatsSecurity>;
export declare function listChatsSecurityToJSON(listChatsSecurity: ListChatsSecurity): string;
/** @internal */
export declare const HasRisk$outboundSchema: z.ZodMiniEnum<typeof HasRisk>;
/** @internal */
export declare const AccountType$outboundSchema: z.ZodMiniEnum<typeof AccountType>;
/** @internal */
export declare const Pinned$outboundSchema: z.ZodMiniEnum<typeof Pinned>;
/** @internal */
export declare const SortBy$outboundSchema: z.ZodMiniEnum<typeof SortBy>;
/** @internal */
export declare const SortOrder$outboundSchema: z.ZodMiniEnum<typeof SortOrder>;
/** @internal */
export type ListChatsRequest$Outbound = {
    search?: string | undefined;
    external_user_id?: string | undefined;
    source?: string | undefined;
    assistant_id?: string | undefined;
    source_kind?: string | undefined;
    exclude_source_kind?: string | undefined;
    has_risk?: string | undefined;
    account_type?: string | undefined;
    pinned?: string | undefined;
    min_risk_score?: number | undefined;
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
export declare const ListChatsRequest$outboundSchema: z.ZodMiniType<ListChatsRequest$Outbound, ListChatsRequest>;
export declare function listChatsRequestToJSON(listChatsRequest: ListChatsRequest): string;
//# sourceMappingURL=listchats.d.ts.map