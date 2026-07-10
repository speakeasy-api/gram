import * as z from "zod/v4-mini";
/**
 * Payload for getting user-level metrics summary
 */
export type GetUserMetricsSummaryPayload = {
    /**
     * Optional account type filter ('team' or 'personal')
     */
    accountType?: string | undefined;
    /**
     * Optional event source filter (e.g. 'hook')
     */
    eventSource?: string | undefined;
    /**
     * Optional filter to a single AI account by its provider org id; scopes metrics to that one account
     */
    externalOrgId?: string | undefined;
    /**
     * External user ID to get metrics for (mutually exclusive with user_id)
     */
    externalUserId?: string | undefined;
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * Optional hook source filter (e.g. 'cursor', 'claude-code')
     */
    hookSource?: string | undefined;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
    /**
     * User ID to get metrics for (mutually exclusive with external_user_id)
     */
    userId?: string | undefined;
};
/** @internal */
export type GetUserMetricsSummaryPayload$Outbound = {
    account_type?: string | undefined;
    event_source?: string | undefined;
    external_org_id?: string | undefined;
    external_user_id?: string | undefined;
    from: string;
    hook_source?: string | undefined;
    to: string;
    user_id?: string | undefined;
};
/** @internal */
export declare const GetUserMetricsSummaryPayload$outboundSchema: z.ZodMiniType<GetUserMetricsSummaryPayload$Outbound, GetUserMetricsSummaryPayload>;
export declare function getUserMetricsSummaryPayloadToJSON(getUserMetricsSummaryPayload: GetUserMetricsSummaryPayload): string;
//# sourceMappingURL=getusermetricssummarypayload.d.ts.map