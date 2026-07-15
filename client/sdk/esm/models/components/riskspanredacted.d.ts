import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RiskSpanRedacted = {
  /**
   * The message field this span matched (see RiskSpan.field).
   */
  field?: string | undefined;
  /**
   * Opaque fingerprint of this span's match, in the same form as RiskResultRedacted.match_redacted.
   */
  matchRedacted: string;
  /**
   * The JSON sub-path within the field for a `.get(...)` match (see RiskSpan.path).
   */
  path?: string | undefined;
  /**
   * Whether this span carried byte-position information.
   */
  positionKnown: boolean;
};
/** @internal */
export declare const RiskSpanRedacted$inboundSchema: z.ZodMiniType<
  RiskSpanRedacted,
  unknown
>;
export declare function riskSpanRedactedFromJSON(
  jsonString: string,
): SafeParseResult<RiskSpanRedacted, SDKValidationError>;
//# sourceMappingURL=riskspanredacted.d.ts.map
