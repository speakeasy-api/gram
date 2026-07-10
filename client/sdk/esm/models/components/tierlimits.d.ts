import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type TierLimits = {
  /**
   * Add-on items bullets of the tier (optional)
   */
  addOnBullets?: Array<string> | undefined;
  /**
   * The base price for the tier
   */
  basePrice: number;
  /**
   * Key feature bullets of the tier
   */
  featureBullets: Array<string>;
  /**
   * Included items bullets of the tier
   */
  includedBullets: Array<string>;
  /**
   * The number of credits included in the tier for playground and other dashboard activities
   */
  includedCredits: number;
  /**
   * The number of servers included in the tier
   */
  includedServers: number;
  /**
   * The number of tool calls included in the tier
   */
  includedToolCalls: number;
  /**
   * The price per additional server
   */
  pricePerAdditionalServer: number;
  /**
   * The price per additional tool call
   */
  pricePerAdditionalToolCall: number;
};
/** @internal */
export declare const TierLimits$inboundSchema: z.ZodMiniType<
  TierLimits,
  unknown
>;
export declare function tierLimitsFromJSON(
  jsonString: string,
): SafeParseResult<TierLimits, SDKValidationError>;
//# sourceMappingURL=tierlimits.d.ts.map
