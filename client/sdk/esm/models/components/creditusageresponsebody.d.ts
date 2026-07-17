import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type CreditUsageResponseBody = {
  /**
   * The number of credits remaining
   */
  creditsUsed: number;
  /**
   * The number of monthly credits
   */
  monthlyCredits: number;
};
/** @internal */
export declare const CreditUsageResponseBody$inboundSchema: z.ZodMiniType<
  CreditUsageResponseBody,
  unknown
>;
export declare function creditUsageResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<CreditUsageResponseBody, SDKValidationError>;
//# sourceMappingURL=creditusageresponsebody.d.ts.map
