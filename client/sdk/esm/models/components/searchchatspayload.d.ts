import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import {
  SearchChatsFilter,
  SearchChatsFilter$Outbound,
} from "./searchchatsfilter.js";
/**
 * Sort order
 */
export declare const SearchChatsPayloadSort: {
  readonly Asc: "asc";
  readonly Desc: "desc";
};
/**
 * Sort order
 */
export type SearchChatsPayloadSort = ClosedEnum<typeof SearchChatsPayloadSort>;
/**
 * Payload for searching chat session summaries
 */
export type SearchChatsPayload = {
  /**
   * Cursor for pagination
   */
  cursor?: string | undefined;
  /**
   * Filter criteria for searching chat sessions
   */
  filter?: SearchChatsFilter | undefined;
  /**
   * Number of items to return (1-1000)
   */
  limit?: number | undefined;
  /**
   * Sort order
   */
  sort?: SearchChatsPayloadSort | undefined;
};
/** @internal */
export declare const SearchChatsPayloadSort$outboundSchema: z.ZodMiniEnum<
  typeof SearchChatsPayloadSort
>;
/** @internal */
export type SearchChatsPayload$Outbound = {
  cursor?: string | undefined;
  filter?: SearchChatsFilter$Outbound | undefined;
  limit: number;
  sort: string;
};
/** @internal */
export declare const SearchChatsPayload$outboundSchema: z.ZodMiniType<
  SearchChatsPayload$Outbound,
  SearchChatsPayload
>;
export declare function searchChatsPayloadToJSON(
  searchChatsPayload: SearchChatsPayload,
): string;
//# sourceMappingURL=searchchatspayload.d.ts.map
