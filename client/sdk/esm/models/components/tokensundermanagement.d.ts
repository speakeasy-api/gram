import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TUMPeriod } from "./tumperiod.js";
export type TokensUnderManagement = {
  /**
   * Email address to notify on TUM threshold events. Only populated for platform admins.
   */
  alertEmail?: string | undefined;
  /**
   * Day of month (1-31) the billing cycle starts, at 00:00 UTC
   */
  billingCycleAnchorDay: number;
  /**
   * TUM usage per billing cycle for the trailing cycles, oldest first. The last entry is the active cycle.
   */
  history: Array<TUMPeriod>;
  /**
   * The contracted monthly tokens under management limit, if one has been configured
   */
  monthlyTokenLimit?: number | undefined;
  /**
   * End of the active billing cycle (exclusive)
   */
  periodEnd: Date;
  /**
   * Start of the active billing cycle
   */
  periodStart: Date;
  /**
   * Tokens under management consumed during the active billing cycle
   */
  tokens: number;
  /**
   * The contracted tunneled MCP server source cap, if one has been configured
   */
  tunneledMcpServerLimit?: number | undefined;
};
/** @internal */
export declare const TokensUnderManagement$inboundSchema: z.ZodMiniType<
  TokensUnderManagement,
  unknown
>;
export declare function tokensUnderManagementFromJSON(
  jsonString: string,
): SafeParseResult<TokensUnderManagement, SDKValidationError>;
//# sourceMappingURL=tokensundermanagement.d.ts.map
