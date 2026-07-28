import * as z from "zod/v4-mini";
/**
 * Filter criteria for searching user usage summaries
 */
export type SearchUsersFilter = {
  /**
   * Optional account type filter ('team' or 'personal').
   */
  accountType?: string | undefined;
  /**
   * Deployment ID filter
   */
  deploymentId?: string | undefined;
  /**
   * Optional event source filter (e.g. 'hook'). When set, only rows with a matching event_source are included.
   */
  eventSource?: string | undefined;
  /**
   * Optional filter to a single AI account by its provider org id (the per-account discriminator); scopes results to that one account.
   */
  externalOrgId?: string | undefined;
  /**
   * Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')
   */
  from: Date;
  /**
   * Optional hook source filter (e.g. 'cursor', 'claude-code').
   */
  hookSource?: string | undefined;
  /**
   * End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')
   */
  to: Date;
  /**
   * Optional list of user identifiers to include. Matches user_id for internal searches and external_user_id for external searches.
   */
  userIds?: Array<string> | undefined;
};
/** @internal */
export type SearchUsersFilter$Outbound = {
  account_type?: string | undefined;
  deployment_id?: string | undefined;
  event_source?: string | undefined;
  external_org_id?: string | undefined;
  from: string;
  hook_source?: string | undefined;
  to: string;
  user_ids?: Array<string> | undefined;
};
/** @internal */
export declare const SearchUsersFilter$outboundSchema: z.ZodMiniType<
  SearchUsersFilter$Outbound,
  SearchUsersFilter
>;
export declare function searchUsersFilterToJSON(
  searchUsersFilter: SearchUsersFilter,
): string;
//# sourceMappingURL=searchusersfilter.d.ts.map
