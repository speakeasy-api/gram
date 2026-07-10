import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { QueryFilter, QueryFilter$Outbound } from "./queryfilter.js";
/**
 * Measure used to rank sessions. Defaults to total_cost.
 */
export declare const SortBy: {
    readonly TotalCost: "total_cost";
    readonly TotalTokens: "total_tokens";
    readonly TotalInputTokens: "total_input_tokens";
    readonly TotalOutputTokens: "total_output_tokens";
    readonly ToolCallCount: "tool_call_count";
    readonly MessageCount: "message_count";
    readonly DurationSeconds: "duration_seconds";
};
/**
 * Measure used to rank sessions. Defaults to total_cost.
 */
export type SortBy = ClosedEnum<typeof SortBy>;
/**
 * Payload for listing org-scoped chat sessions
 */
export type ListSessionsPayload = {
    /**
     * Opaque cursor for pagination
     */
    cursor?: string | undefined;
    /**
     * Optional filters; all filters are ANDed together.
     */
    filters?: Array<QueryFilter> | undefined;
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * Number of sessions to return (1-1000)
     */
    limit?: number | undefined;
    /**
     * Measure used to rank sessions. Defaults to total_cost.
     */
    sortBy?: SortBy | undefined;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
};
/** @internal */
export declare const SortBy$outboundSchema: z.ZodMiniEnum<typeof SortBy>;
/** @internal */
export type ListSessionsPayload$Outbound = {
    cursor?: string | undefined;
    filters?: Array<QueryFilter$Outbound> | undefined;
    from: string;
    limit: number;
    sort_by: string;
    to: string;
};
/** @internal */
export declare const ListSessionsPayload$outboundSchema: z.ZodMiniType<ListSessionsPayload$Outbound, ListSessionsPayload>;
export declare function listSessionsPayloadToJSON(listSessionsPayload: ListSessionsPayload): string;
//# sourceMappingURL=listsessionspayload.d.ts.map