import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Tool usage filter option type
 */
export declare const OptionTypes: {
  readonly HostedServers: "hosted_servers";
  readonly ShadowServers: "shadow_servers";
  readonly Users: "users";
};
/**
 * Tool usage filter option type
 */
export type OptionTypes = ClosedEnum<typeof OptionTypes>;
/**
 * Payload for target-aware MCP and tool usage filter options
 */
export type GetToolUsageFilterOptionsPayload = {
  /**
   * Start time in ISO 8601 format
   */
  from: Date;
  /**
   * Filter option types to include. Empty means all option types.
   */
  optionTypes?: Array<OptionTypes> | undefined;
  /**
   * End time in ISO 8601 format
   */
  to: Date;
};
/** @internal */
export declare const OptionTypes$outboundSchema: z.ZodMiniEnum<
  typeof OptionTypes
>;
/** @internal */
export type GetToolUsageFilterOptionsPayload$Outbound = {
  from: string;
  option_types?: Array<string> | undefined;
  to: string;
};
/** @internal */
export declare const GetToolUsageFilterOptionsPayload$outboundSchema: z.ZodMiniType<
  GetToolUsageFilterOptionsPayload$Outbound,
  GetToolUsageFilterOptionsPayload
>;
export declare function getToolUsageFilterOptionsPayloadToJSON(
  getToolUsageFilterOptionsPayload: GetToolUsageFilterOptionsPayload,
): string;
//# sourceMappingURL=gettoolusagefilteroptionspayload.d.ts.map
