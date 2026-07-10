import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Aggregated skills metrics for a single skill
 */
export type SkillSummary = {
    /**
     * Skill name (extracted from tool name)
     */
    skillName: string;
    /**
     * Number of unique users who used this skill
     */
    uniqueUsers: number;
    /**
     * Total number of times this skill was used
     */
    useCount: number;
};
/** @internal */
export declare const SkillSummary$inboundSchema: z.ZodMiniType<SkillSummary, unknown>;
export declare function skillSummaryFromJSON(jsonString: string): SafeParseResult<SkillSummary, SDKValidationError>;
//# sourceMappingURL=skillsummary.d.ts.map