import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * How match_value is interpreted.
 */
export declare const MatchType: {
  readonly Exact: "exact";
  readonly Regex: "regex";
  readonly RuleId: "rule_id";
  readonly Source: "source";
  readonly EntityType: "entity_type";
};
/**
 * How match_value is interpreted.
 */
export type MatchType = ClosedEnum<typeof MatchType>;
export type CreateRiskExclusionRequestBody = {
  /**
   * Whether the exclusion is active.
   */
  enabled?: boolean | undefined;
  /**
   * How match_value is interpreted.
   */
  matchType: MatchType;
  /**
   * The value matched against findings, interpreted per match_type.
   */
  matchValue: string;
  /**
   * Bind the exclusion to a single policy. Omit for a global (project-wide) exclusion.
   */
  riskPolicyId?: string | undefined;
  /**
   * Optional: only apply within this rule_id. Empty means any.
   */
  ruleIdFilter?: string | undefined;
  /**
   * Optional: only apply within this source. Empty means any.
   */
  sourceFilter?: string | undefined;
};
/** @internal */
export declare const MatchType$outboundSchema: z.ZodMiniEnum<typeof MatchType>;
/** @internal */
export type CreateRiskExclusionRequestBody$Outbound = {
  enabled: boolean;
  match_type: string;
  match_value: string;
  risk_policy_id?: string | undefined;
  rule_id_filter: string;
  source_filter: string;
};
/** @internal */
export declare const CreateRiskExclusionRequestBody$outboundSchema: z.ZodMiniType<
  CreateRiskExclusionRequestBody$Outbound,
  CreateRiskExclusionRequestBody
>;
export declare function createRiskExclusionRequestBodyToJSON(
  createRiskExclusionRequestBody: CreateRiskExclusionRequestBody,
): string;
//# sourceMappingURL=createriskexclusionrequestbody.d.ts.map
