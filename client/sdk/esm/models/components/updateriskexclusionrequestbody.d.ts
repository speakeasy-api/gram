import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * How match_value is interpreted.
 */
export declare const UpdateRiskExclusionRequestBodyMatchType: {
    readonly Exact: "exact";
    readonly Regex: "regex";
    readonly RuleId: "rule_id";
    readonly Source: "source";
    readonly EntityType: "entity_type";
};
/**
 * How match_value is interpreted.
 */
export type UpdateRiskExclusionRequestBodyMatchType = ClosedEnum<typeof UpdateRiskExclusionRequestBodyMatchType>;
export type UpdateRiskExclusionRequestBody = {
    /**
     * Whether the exclusion is active. Omit to leave unchanged.
     */
    enabled?: boolean | undefined;
    /**
     * The exclusion ID.
     */
    id: string;
    /**
     * How match_value is interpreted.
     */
    matchType: UpdateRiskExclusionRequestBodyMatchType;
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
export declare const UpdateRiskExclusionRequestBodyMatchType$outboundSchema: z.ZodMiniEnum<typeof UpdateRiskExclusionRequestBodyMatchType>;
/** @internal */
export type UpdateRiskExclusionRequestBody$Outbound = {
    enabled?: boolean | undefined;
    id: string;
    match_type: string;
    match_value: string;
    risk_policy_id?: string | undefined;
    rule_id_filter: string;
    source_filter: string;
};
/** @internal */
export declare const UpdateRiskExclusionRequestBody$outboundSchema: z.ZodMiniType<UpdateRiskExclusionRequestBody$Outbound, UpdateRiskExclusionRequestBody>;
export declare function updateRiskExclusionRequestBodyToJSON(updateRiskExclusionRequestBody: UpdateRiskExclusionRequestBody): string;
//# sourceMappingURL=updateriskexclusionrequestbody.d.ts.map