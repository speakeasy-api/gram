import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type TestDetectionRuleMatch = {
  /**
   * Confidence score in the range 0.0 to 1.0.
   */
  confidence: number;
  /**
   * Human-readable description of why this match was flagged.
   */
  description?: string | undefined;
  /**
   * Exclusive end byte offset of the match in the sample.
   */
  endPos: number;
  /**
   * Matched substring of the sample.
   */
  match: string;
  /**
   * Canonical rule id of the match (may differ from the requested rule id when one input matches multiple rules).
   */
  ruleId: string;
  /**
   * Detection source (e.g. `gitleaks`, `presidio`, `prompt_injection`, `custom`).
   */
  source: string;
  /**
   * Inclusive start byte offset of the match in the sample.
   */
  startPos: number;
  /**
   * Tags from the underlying rule.
   */
  tags?: Array<string> | undefined;
};
/** @internal */
export declare const TestDetectionRuleMatch$inboundSchema: z.ZodMiniType<
  TestDetectionRuleMatch,
  unknown
>;
export declare function testDetectionRuleMatchFromJSON(
  jsonString: string,
): SafeParseResult<TestDetectionRuleMatch, SDKValidationError>;
//# sourceMappingURL=testdetectionrulematch.d.ts.map
