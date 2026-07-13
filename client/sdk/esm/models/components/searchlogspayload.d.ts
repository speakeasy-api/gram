import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { LogFilter, LogFilter$Outbound } from "./logfilter.js";
import {
  SearchLogsFilter,
  SearchLogsFilter$Outbound,
} from "./searchlogsfilter.js";
/**
 * Sort order
 */
export declare const SearchLogsPayloadSort: {
  readonly Asc: "asc";
  readonly Desc: "desc";
};
/**
 * Sort order
 */
export type SearchLogsPayloadSort = ClosedEnum<typeof SearchLogsPayloadSort>;
/**
 * Payload for searching telemetry logs
 */
export type SearchLogsPayload = {
  /**
   * Cursor for pagination
   */
  cursor?: string | undefined;
  /**
   * Filter criteria for searching logs
   */
  filter?: SearchLogsFilter | undefined;
  /**
   * Filter conditions for the search query
   */
  filters?: Array<LogFilter> | undefined;
  /**
   * Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')
   */
  from?: Date | undefined;
  /**
   * Number of items to return (1-1000)
   */
  limit?: number | undefined;
  /**
   * Sort order
   */
  sort?: SearchLogsPayloadSort | undefined;
  /**
   * End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')
   */
  to?: Date | undefined;
};
/** @internal */
export declare const SearchLogsPayloadSort$outboundSchema: z.ZodMiniEnum<
  typeof SearchLogsPayloadSort
>;
/** @internal */
export type SearchLogsPayload$Outbound = {
  cursor?: string | undefined;
  filter?: SearchLogsFilter$Outbound | undefined;
  filters?: Array<LogFilter$Outbound> | undefined;
  from?: string | undefined;
  limit: number;
  sort: string;
  to?: string | undefined;
};
/** @internal */
export declare const SearchLogsPayload$outboundSchema: z.ZodMiniType<
  SearchLogsPayload$Outbound,
  SearchLogsPayload
>;
export declare function searchLogsPayloadToJSON(
  searchLogsPayload: SearchLogsPayload,
): string;
//# sourceMappingURL=searchlogspayload.d.ts.map
