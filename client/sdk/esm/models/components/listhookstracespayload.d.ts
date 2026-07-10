import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { LogFilter, LogFilter$Outbound } from "./logfilter.js";
/**
 * Sort order
 */
export declare const Sort: {
    readonly Asc: "asc";
    readonly Desc: "desc";
};
/**
 * Sort order
 */
export type Sort = ClosedEnum<typeof Sort>;
export declare const ListHooksTracesPayloadTypesToInclude: {
    readonly Mcp: "mcp";
    readonly Local: "local";
    readonly Skill: "skill";
};
export type ListHooksTracesPayloadTypesToInclude = ClosedEnum<typeof ListHooksTracesPayloadTypesToInclude>;
/**
 * Payload for listing hook traces
 */
export type ListHooksTracesPayload = {
    /**
     * Cursor for pagination (trace_id)
     */
    cursor?: string | undefined;
    /**
     * Filter conditions for the search query
     */
    filters?: Array<LogFilter> | undefined;
    /**
     * Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')
     */
    from: Date;
    /**
     * Number of items to return (1-1000)
     */
    limit?: number | undefined;
    /**
     * Sort order
     */
    sort?: Sort | undefined;
    /**
     * End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')
     */
    to: Date;
    /**
     * Hook types to include (mcp, local, skill). If empty or not provided, includes all types.
     */
    typesToInclude?: Array<ListHooksTracesPayloadTypesToInclude> | undefined;
};
/** @internal */
export declare const Sort$outboundSchema: z.ZodMiniEnum<typeof Sort>;
/** @internal */
export declare const ListHooksTracesPayloadTypesToInclude$outboundSchema: z.ZodMiniEnum<typeof ListHooksTracesPayloadTypesToInclude>;
/** @internal */
export type ListHooksTracesPayload$Outbound = {
    cursor?: string | undefined;
    filters?: Array<LogFilter$Outbound> | undefined;
    from: string;
    limit: number;
    sort: string;
    to: string;
    types_to_include?: Array<string> | undefined;
};
/** @internal */
export declare const ListHooksTracesPayload$outboundSchema: z.ZodMiniType<ListHooksTracesPayload$Outbound, ListHooksTracesPayload>;
export declare function listHooksTracesPayloadToJSON(listHooksTracesPayload: ListHooksTracesPayload): string;
//# sourceMappingURL=listhookstracespayload.d.ts.map