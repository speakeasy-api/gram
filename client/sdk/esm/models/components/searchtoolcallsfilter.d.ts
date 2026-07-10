import * as z from "zod/v4-mini";
/**
 * Filter criteria for searching tool calls
 */
export type SearchToolCallsFilter = {
    /**
     * Deployment ID filter
     */
    deploymentId?: string | undefined;
    /**
     * Event source filter (e.g., 'hook', 'tool_call', 'chat_completion')
     */
    eventSource?: string | undefined;
    /**
     * Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')
     */
    from?: Date | undefined;
    /**
     * Function ID filter
     */
    functionId?: string | undefined;
    /**
     * Gram URN filter (single URN, use gram_urns for multiple)
     */
    gramUrn?: string | undefined;
    /**
     * End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')
     */
    to?: Date | undefined;
};
/** @internal */
export type SearchToolCallsFilter$Outbound = {
    deployment_id?: string | undefined;
    event_source?: string | undefined;
    from?: string | undefined;
    function_id?: string | undefined;
    gram_urn?: string | undefined;
    to?: string | undefined;
};
/** @internal */
export declare const SearchToolCallsFilter$outboundSchema: z.ZodMiniType<SearchToolCallsFilter$Outbound, SearchToolCallsFilter>;
export declare function searchToolCallsFilterToJSON(searchToolCallsFilter: SearchToolCallsFilter): string;
//# sourceMappingURL=searchtoolcallsfilter.d.ts.map