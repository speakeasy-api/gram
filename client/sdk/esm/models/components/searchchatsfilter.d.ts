import * as z from "zod/v4-mini";
/**
 * Filter criteria for searching chat sessions
 */
export type SearchChatsFilter = {
    /**
     * Deployment ID filter
     */
    deploymentId?: string | undefined;
    /**
     * External user ID filter
     */
    externalUserId?: string | undefined;
    /**
     * Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')
     */
    from?: Date | undefined;
    /**
     * Gram URN filter (single URN, use gram_urns for multiple)
     */
    gramUrn?: string | undefined;
    /**
     * End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')
     */
    to?: Date | undefined;
    /**
     * User ID filter
     */
    userId?: string | undefined;
};
/** @internal */
export type SearchChatsFilter$Outbound = {
    deployment_id?: string | undefined;
    external_user_id?: string | undefined;
    from?: string | undefined;
    gram_urn?: string | undefined;
    to?: string | undefined;
    user_id?: string | undefined;
};
/** @internal */
export declare const SearchChatsFilter$outboundSchema: z.ZodMiniType<SearchChatsFilter$Outbound, SearchChatsFilter>;
export declare function searchChatsFilterToJSON(searchChatsFilter: SearchChatsFilter): string;
//# sourceMappingURL=searchchatsfilter.d.ts.map