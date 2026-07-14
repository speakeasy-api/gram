import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskUnmaskResultResult = {
  /**
   * The risk result ID.
   */
  id: string;
  /**
   * The plaintext matched secret or sensitive data for this result. Empty string when the finding has no top-level match (e.g. a spans-only finding).
   */
  match: string;
};
/** @internal */
export declare const RiskUnmaskResultResult$inboundSchema: z.ZodMiniType<
  RiskUnmaskResultResult,
  unknown
>;
export declare function riskUnmaskResultResultFromJSON(
  jsonString: string,
): SafeParseResult<RiskUnmaskResultResult, SDKValidationError>;
//# sourceMappingURL=riskunmaskresultresult.d.ts.map
