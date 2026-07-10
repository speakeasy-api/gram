import * as z from "zod/v4-mini";
export type SetBillingMetadataRequestBody = {
  /**
   * Email address to notify on TUM threshold events. Omit to clear.
   */
  alertEmail?: string | undefined;
  /**
   * Day of month (1-31) the billing cycle starts, at 00:00 UTC
   */
  billingCycleAnchorDay: number;
  /**
   * The contracted monthly tokens under management limit. Omit to clear.
   */
  monthlyTokenLimit?: number | undefined;
  /**
   * The contracted tunneled MCP server source cap. Omit to leave the configured value unchanged; never-configured orgs use the plan default.
   */
  tunneledMcpServerLimit?: number | undefined;
};
/** @internal */
export type SetBillingMetadataRequestBody$Outbound = {
  alert_email?: string | undefined;
  billing_cycle_anchor_day: number;
  monthly_token_limit?: number | undefined;
  tunneled_mcp_server_limit?: number | undefined;
};
/** @internal */
export declare const SetBillingMetadataRequestBody$outboundSchema: z.ZodMiniType<
  SetBillingMetadataRequestBody$Outbound,
  SetBillingMetadataRequestBody
>;
export declare function setBillingMetadataRequestBodyToJSON(
  setBillingMetadataRequestBody: SetBillingMetadataRequestBody,
): string;
//# sourceMappingURL=setbillingmetadatarequestbody.d.ts.map
