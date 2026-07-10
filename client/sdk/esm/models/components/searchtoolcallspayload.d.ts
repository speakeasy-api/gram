import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { SearchToolCallsFilter, SearchToolCallsFilter$Outbound } from "./searchtoolcallsfilter.js";
/**
 * Sort order
 */
export declare const SearchToolCallsPayloadSort: {
    readonly Asc: "asc";
    readonly Desc: "desc";
};
/**
 * Sort order
 */
export type SearchToolCallsPayloadSort = ClosedEnum<typeof SearchToolCallsPayloadSort>;
/**
 * Payload for searching tool call summaries
 */
export type SearchToolCallsPayload = {
    /**
     * Cursor for pagination
     */
    cursor?: string | undefined;
    /**
     * Filter criteria for searching tool calls
     */
    filter?: SearchToolCallsFilter | undefined;
    /**
     * Number of items to return (1-1000)
     */
    limit?: number | undefined;
    /**
     * Sort order
     */
    sort?: SearchToolCallsPayloadSort | undefined;
};
/** @internal */
export declare const SearchToolCallsPayloadSort$outboundSchema: z.ZodMiniEnum<typeof SearchToolCallsPayloadSort>;
/** @internal */
export type SearchToolCallsPayload$Outbound = {
    cursor?: string | undefined;
    filter?: SearchToolCallsFilter$Outbound | undefined;
    limit: number;
    sort: string;
};
/** @internal */
export declare const SearchToolCallsPayload$outboundSchema: z.ZodMiniType<SearchToolCallsPayload$Outbound, SearchToolCallsPayload>;
export declare function searchToolCallsPayloadToJSON(searchToolCallsPayload: SearchToolCallsPayload): string;
//# sourceMappingURL=searchtoolcallspayload.d.ts.map