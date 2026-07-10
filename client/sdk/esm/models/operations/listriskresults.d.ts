import * as z from "zod/v4-mini";
export type ListRiskResultsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListRiskResultsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListRiskResultsSecurity = {
    option1?: ListRiskResultsSecurityOption1 | undefined;
    option2?: ListRiskResultsSecurityOption2 | undefined;
};
export type ListRiskResultsRequest = {
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
export type ListRiskResultsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskResultsSecurityOption1$outboundSchema: z.ZodMiniType<ListRiskResultsSecurityOption1$Outbound, ListRiskResultsSecurityOption1>;
export declare function listRiskResultsSecurityOption1ToJSON(listRiskResultsSecurityOption1: ListRiskResultsSecurityOption1): string;
/** @internal */
export type ListRiskResultsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskResultsSecurityOption2$outboundSchema: z.ZodMiniType<ListRiskResultsSecurityOption2$Outbound, ListRiskResultsSecurityOption2>;
export declare function listRiskResultsSecurityOption2ToJSON(listRiskResultsSecurityOption2: ListRiskResultsSecurityOption2): string;
/** @internal */
export type ListRiskResultsSecurity$Outbound = {
    Option1?: ListRiskResultsSecurityOption1$Outbound | undefined;
    Option2?: ListRiskResultsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskResultsSecurity$outboundSchema: z.ZodMiniType<ListRiskResultsSecurity$Outbound, ListRiskResultsSecurity>;
export declare function listRiskResultsSecurityToJSON(listRiskResultsSecurity: ListRiskResultsSecurity): string;
/** @internal */
export type ListRiskResultsRequest$Outbound = {
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
export declare const ListRiskResultsRequest$outboundSchema: z.ZodMiniType<ListRiskResultsRequest$Outbound, ListRiskResultsRequest>;
export declare function listRiskResultsRequestToJSON(listRiskResultsRequest: ListRiskResultsRequest): string;
//# sourceMappingURL=listriskresults.d.ts.map