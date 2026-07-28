import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * One rule in the built-in exclusion library. Deliberately omits internal detection-engine identifiers (sources, rule ids) so they are not exposed to end users.
 */
export type BuiltinExclusionEntry = {
  /**
   * Human rationale for why these values are known-safe.
   */
  description: string;
  /**
   * Stable rule id.
   */
  id: string;
  /**
   * Label surfaced when this rule suppresses a finding.
   */
  reason: string;
  /**
   * Example values — published test/documentation data, never real secrets.
   */
  samples?: Array<string> | undefined;
};
/** @internal */
export declare const BuiltinExclusionEntry$inboundSchema: z.ZodMiniType<
  BuiltinExclusionEntry,
  unknown
>;
export declare function builtinExclusionEntryFromJSON(
  jsonString: string,
): SafeParseResult<BuiltinExclusionEntry, SDKValidationError>;
//# sourceMappingURL=builtinexclusionentry.d.ts.map
