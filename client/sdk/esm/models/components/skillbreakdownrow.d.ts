import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Per-(skill, user) aggregated counts
 */
export type SkillBreakdownRow = {
  /**
   * Skill name
   */
  skillName: string;
  /**
   * Use count for this skill/user combination
   */
  useCount: number;
  /**
   * User email address
   */
  userEmail: string;
};
/** @internal */
export declare const SkillBreakdownRow$inboundSchema: z.ZodMiniType<
  SkillBreakdownRow,
  unknown
>;
export declare function skillBreakdownRowFromJSON(
  jsonString: string,
): SafeParseResult<SkillBreakdownRow, SDKValidationError>;
//# sourceMappingURL=skillbreakdownrow.d.ts.map
