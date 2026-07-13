import * as z from "zod/v4-mini";
/**
 * Payload for getting observability overview metrics
 */
export type GetObservabilityOverviewPayload = {
  /**
   * Optional account type filter ('team' or 'personal')
   */
  accountType?: string | undefined;
  /**
   * Optional API key ID filter
   */
  apiKeyId?: string | undefined;
  /**
   * Optional event source filter (e.g. 'hook')
   */
  eventSource?: string | undefined;
  /**
   * Optional filter to a single AI account by its provider org id; scopes the overview to that one account
   */
  externalOrgId?: string | undefined;
  /**
   * Optional external user ID filter
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
   * Whether to include time series data (default: true)
   */
  includeTimeSeries?: boolean | undefined;
  /**
   * Optional MCP server ID filter (fronting server; spans both remote-backed and toolset-backed activity)
   */
  mcpServerId?: string | undefined;
  /**
   * Optional Remote MCP server ID filter
   */
  remoteMcpServerId?: string | undefined;
  /**
   * End time in ISO 8601 format
   */
  to: Date;
  /**
   * Optional toolset/MCP server slug filter
   */
  toolsetSlug?: string | undefined;
  /**
   * Optional internal user ID filter
   */
  userId?: string | undefined;
};
/** @internal */
export type GetObservabilityOverviewPayload$Outbound = {
  account_type?: string | undefined;
  api_key_id?: string | undefined;
  event_source?: string | undefined;
  external_org_id?: string | undefined;
  external_user_id?: string | undefined;
  from: string;
  hook_source?: string | undefined;
  include_time_series: boolean;
  mcp_server_id?: string | undefined;
  remote_mcp_server_id?: string | undefined;
  to: string;
  toolset_slug?: string | undefined;
  user_id?: string | undefined;
};
/** @internal */
export declare const GetObservabilityOverviewPayload$outboundSchema: z.ZodMiniType<
  GetObservabilityOverviewPayload$Outbound,
  GetObservabilityOverviewPayload
>;
export declare function getObservabilityOverviewPayloadToJSON(
  getObservabilityOverviewPayload: GetObservabilityOverviewPayload,
): string;
//# sourceMappingURL=getobservabilityoverviewpayload.d.ts.map
