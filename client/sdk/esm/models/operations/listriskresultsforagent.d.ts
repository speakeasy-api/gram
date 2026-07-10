import * as z from "zod/v4-mini";
export type ListRiskResultsForAgentSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListRiskResultsForAgentSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListRiskResultsForAgentSecurity = {
    option1?: ListRiskResultsForAgentSecurityOption1 | undefined;
    option2?: ListRiskResultsForAgentSecurityOption2 | undefined;
};
export type ListRiskResultsForAgentRequest = {
    /**
     * Optional policy ID to filter by.
     */
    policyId?: string | undefined;
    /**
     * Optional chat ID to filter by.
     */
    chatId?: string | undefined;
    /**
     * Optional rule category key to filter by (e.g. secrets, pii, financial).
     */
    category?: string | undefined;
    /**
     * Optional rule identifier substring to filter by (case-insensitive, e.g. 'secret' matches all 'secret.*' rules).
     */
    ruleId?: string | undefined;
    /**
     * Optional user identifier substring to filter by (case-insensitive, matched against the chat's external user id).
     */
    userId?: string | undefined;
    /**
     * If true, collapse results to one row per (policy_id, rule_id, match), keeping the most recent occurrence. Useful when the same secret is detected many times within a single message body.
     */
    uniqueMatch?: boolean | undefined;
    /**
     * Filter results to messages created at or after this timestamp (ISO 8601).
     */
    from?: Date | undefined;
    /**
     * Filter results to messages created strictly before this timestamp (ISO 8601).
     */
    to?: Date | undefined;
    /**
     * Cursor to fetch the next page of results.
     */
    cursor?: string | undefined;
    /**
     * Maximum number of results to return per page.
     */
    limit?: number | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
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
export type ListRiskResultsForAgentSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskResultsForAgentSecurityOption1$outboundSchema: z.ZodMiniType<ListRiskResultsForAgentSecurityOption1$Outbound, ListRiskResultsForAgentSecurityOption1>;
export declare function listRiskResultsForAgentSecurityOption1ToJSON(listRiskResultsForAgentSecurityOption1: ListRiskResultsForAgentSecurityOption1): string;
/** @internal */
export type ListRiskResultsForAgentSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskResultsForAgentSecurityOption2$outboundSchema: z.ZodMiniType<ListRiskResultsForAgentSecurityOption2$Outbound, ListRiskResultsForAgentSecurityOption2>;
export declare function listRiskResultsForAgentSecurityOption2ToJSON(listRiskResultsForAgentSecurityOption2: ListRiskResultsForAgentSecurityOption2): string;
/** @internal */
export type ListRiskResultsForAgentSecurity$Outbound = {
    Option1?: ListRiskResultsForAgentSecurityOption1$Outbound | undefined;
    Option2?: ListRiskResultsForAgentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskResultsForAgentSecurity$outboundSchema: z.ZodMiniType<ListRiskResultsForAgentSecurity$Outbound, ListRiskResultsForAgentSecurity>;
export declare function listRiskResultsForAgentSecurityToJSON(listRiskResultsForAgentSecurity: ListRiskResultsForAgentSecurity): string;
/** @internal */
export type ListRiskResultsForAgentRequest$Outbound = {
    policy_id?: string | undefined;
    chat_id?: string | undefined;
    category?: string | undefined;
    rule_id?: string | undefined;
    user_id?: string | undefined;
    unique_match?: boolean | undefined;
    from?: string | undefined;
    to?: string | undefined;
    cursor?: string | undefined;
    limit?: number | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRiskResultsForAgentRequest$outboundSchema: z.ZodMiniType<ListRiskResultsForAgentRequest$Outbound, ListRiskResultsForAgentRequest>;
export declare function listRiskResultsForAgentRequestToJSON(listRiskResultsForAgentRequest: ListRiskResultsForAgentRequest): string;
//# sourceMappingURL=listriskresultsforagent.d.ts.map