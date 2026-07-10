import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * How match_value is interpreted: exact (finding text), regex (RE2 pattern over finding text), rule_id, source, or entity_type (presidio entity, matched as rule_id 'pii.<entity>').
 */
export declare const RiskExclusionMatchType: {
  readonly Exact: "exact";
  readonly Regex: "regex";
  readonly RuleId: "rule_id";
  readonly Source: "source";
  readonly EntityType: "entity_type";
};
/**
 * How match_value is interpreted: exact (finding text), regex (RE2 pattern over finding text), rule_id, source, or entity_type (presidio entity, matched as rule_id 'pii.<entity>').
 */
export type RiskExclusionMatchType = ClosedEnum<typeof RiskExclusionMatchType>;
export type RiskExclusion = {
  /**
   * When the exclusion was created.
   */
  createdAt: Date;
  /**
   * Whether the exclusion is active.
   */
  enabled: boolean;
  /**
   * The exclusion ID.
   */
  id: string;
  /**
   * How match_value is interpreted: exact (finding text), regex (RE2 pattern over finding text), rule_id, source, or entity_type (presidio entity, matched as rule_id 'pii.<entity>').
   */
  matchType: RiskExclusionMatchType;
  /**
   * The value matched against findings, interpreted per match_type.
   */
  matchValue: string;
  /**
   * The project ID.
   */
  projectId: string;
  /**
   * The policy this exclusion is bound to. Null/omitted means global: the exclusion applies to every policy in the project.
   */
  riskPolicyId?: string | undefined;
  /**
   * Optional narrowing: an exact/regex/source exclusion only applies to findings with this rule_id. Empty means any.
   */
  ruleIdFilter: string;
  /**
   * Optional narrowing: an exact/regex/rule_id exclusion only applies to findings from this source. Empty means any.
   */
  sourceFilter: string;
  /**
   * When the exclusion was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const RiskExclusionMatchType$inboundSchema: z.ZodMiniEnum<
  typeof RiskExclusionMatchType
>;
/** @internal */
export declare const RiskExclusion$inboundSchema: z.ZodMiniType<
  RiskExclusion,
  unknown
>;
export declare function riskExclusionFromJSON(
  jsonString: string,
): SafeParseResult<RiskExclusion, SDKValidationError>;
//# sourceMappingURL=riskexclusion.d.ts.map
