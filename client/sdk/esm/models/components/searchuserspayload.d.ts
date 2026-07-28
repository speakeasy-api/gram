import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import {
  SearchUsersFilter,
  SearchUsersFilter$Outbound,
} from "./searchusersfilter.js";
/**
 * Grouping dimension for results
 */
export declare const SearchUsersPayloadGroupBy: {
  readonly Employee: "employee";
  readonly Role: "role";
};
/**
 * Grouping dimension for results
 */
export type SearchUsersPayloadGroupBy = ClosedEnum<
  typeof SearchUsersPayloadGroupBy
>;
/**
 * Sort order
 */
export declare const SearchUsersPayloadSort: {
  readonly Asc: "asc";
  readonly Desc: "desc";
};
/**
 * Sort order
 */
export type SearchUsersPayloadSort = ClosedEnum<typeof SearchUsersPayloadSort>;
/**
 * Type of user identifier to group by
 */
export declare const SearchUsersPayloadUserType: {
  readonly Internal: "internal";
  readonly External: "external";
};
/**
 * Type of user identifier to group by
 */
export type SearchUsersPayloadUserType = ClosedEnum<
  typeof SearchUsersPayloadUserType
>;
/**
 * Payload for searching user usage summaries
 */
export type SearchUsersPayload = {
  /**
   * Cursor for pagination (user identifier from last item)
   */
  cursor?: string | undefined;
  /**
   * Filter criteria for searching user usage summaries
   */
  filter: SearchUsersFilter;
  /**
   * Grouping dimension for results
   */
  groupBy?: SearchUsersPayloadGroupBy | undefined;
  /**
   * Number of items to return (1-1000)
   */
  limit?: number | undefined;
  /**
   * Sort order
   */
  sort?: SearchUsersPayloadSort | undefined;
  /**
   * Type of user identifier to group by
   */
  userType: SearchUsersPayloadUserType;
};
/** @internal */
export declare const SearchUsersPayloadGroupBy$outboundSchema: z.ZodMiniEnum<
  typeof SearchUsersPayloadGroupBy
>;
/** @internal */
export declare const SearchUsersPayloadSort$outboundSchema: z.ZodMiniEnum<
  typeof SearchUsersPayloadSort
>;
/** @internal */
export declare const SearchUsersPayloadUserType$outboundSchema: z.ZodMiniEnum<
  typeof SearchUsersPayloadUserType
>;
/** @internal */
export type SearchUsersPayload$Outbound = {
  cursor?: string | undefined;
  filter: SearchUsersFilter$Outbound;
  group_by: string;
  limit: number;
  sort: string;
  user_type: string;
};
/** @internal */
export declare const SearchUsersPayload$outboundSchema: z.ZodMiniType<
  SearchUsersPayload$Outbound,
  SearchUsersPayload
>;
export declare function searchUsersPayloadToJSON(
  searchUsersPayload: SearchUsersPayload,
): string;
//# sourceMappingURL=searchuserspayload.d.ts.map
