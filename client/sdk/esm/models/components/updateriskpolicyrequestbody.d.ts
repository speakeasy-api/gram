import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import {
  RiskPolicyModelConfig,
  RiskPolicyModelConfig$Outbound,
} from "./riskpolicymodelconfig.js";
/**
 * Policy action: flag or block.
 */
export declare const UpdateRiskPolicyRequestBodyAction: {
  readonly Flag: "flag";
  readonly Block: "block";
};
/**
 * Policy action: flag or block.
 */
export type UpdateRiskPolicyRequestBodyAction = ClosedEnum<
  typeof UpdateRiskPolicyRequestBodyAction
>;
/**
 * Policy audience type: everyone or targeted. Omit to preserve the current audience type.
 */
export declare const UpdateRiskPolicyRequestBodyAudienceType: {
  readonly Everyone: "everyone";
  readonly Targeted: "targeted";
};
/**
 * Policy audience type: everyone or targeted. Omit to preserve the current audience type.
 */
export type UpdateRiskPolicyRequestBodyAudienceType = ClosedEnum<
  typeof UpdateRiskPolicyRequestBodyAudienceType
>;
export type UpdateRiskPolicyRequestBody = {
  /**
   * Policy action: flag or block.
   */
  action?: UpdateRiskPolicyRequestBodyAction | undefined;
  /**
   * For the account_identity source: corporate email domains considered approved. Omit to preserve the current list; send an empty array to clear it.
   */
  approvedEmailDomains?: Array<string> | undefined;
  /**
   * Principal URNs this policy applies to. Omit to preserve the current target principals.
   */
  audiencePrincipalUrns?: Array<string> | undefined;
  /**
   * Policy audience type: everyone or targeted. Omit to preserve the current audience type.
   */
  audienceType?: UpdateRiskPolicyRequestBodyAudienceType | undefined;
  /**
   * Whether the policy name should be auto-generated.
   */
  autoName?: boolean | undefined;
  /**
   * Custom detection rule ids to attach as detectors: a match produces a finding. Omit to preserve the current selection.
   */
  customRuleIds?: Array<string> | undefined;
  /**
   * Canonical rule_ids the user has unchecked within otherwise-enabled categories. Matching findings are dropped at scan time.
   */
  disabledRules?: Array<string> | undefined;
  /**
   * Whether the policy is active.
   */
  enabled?: boolean | undefined;
  /**
   * The policy ID.
   */
  id: string;
  /**
   * Message types this policy applies to. Omit to preserve the current selection; send an empty array to apply to all types.
   */
  messageTypes?: Array<string> | undefined;
  modelConfig?: RiskPolicyModelConfig | undefined;
  /**
   * The policy name.
   */
  name: string;
  /**
   * Presidio entity types to detect.
   */
  presidioEntities?: Array<string> | undefined;
  /**
   * Minimum Presidio confidence (0.0-1.0) a PII match must clear to surface. Omit/null applies the default (0.5).
   */
  presidioScoreThreshold?: number | undefined;
  /**
   * For prompt_based policies: the guardrail prompt the LLM judge evaluates each in-scope message against. Omit to preserve the current value.
   */
  prompt?: string | undefined;
  /**
   * Prompt-injection detection rule ids to enable in addition to the heuristic baseline.
   */
  promptInjectionRules?: Array<string> | undefined;
  /**
   * CEL exemption predicate. Omit to preserve the current value; send empty to clear.
   */
  scopeExempt?: string | undefined;
  /**
   * CEL scope predicate (in addition to message_types). Omit to preserve the current value; send empty to clear.
   */
  scopeInclude?: string | undefined;
  /**
   * Detection sources to enable.
   */
  sources?: Array<string> | undefined;
  /**
   * Optional message shown to end users when this policy blocks an action or surfaces a flagged finding. Send an empty string to clear.
   */
  userMessage?: string | undefined;
};
/** @internal */
export declare const UpdateRiskPolicyRequestBodyAction$outboundSchema: z.ZodMiniEnum<
  typeof UpdateRiskPolicyRequestBodyAction
>;
/** @internal */
export declare const UpdateRiskPolicyRequestBodyAudienceType$outboundSchema: z.ZodMiniEnum<
  typeof UpdateRiskPolicyRequestBodyAudienceType
>;
/** @internal */
export type UpdateRiskPolicyRequestBody$Outbound = {
  action?: string | undefined;
  approved_email_domains?: Array<string> | undefined;
  audience_principal_urns?: Array<string> | undefined;
  audience_type?: string | undefined;
  auto_name?: boolean | undefined;
  custom_rule_ids?: Array<string> | undefined;
  disabled_rules?: Array<string> | undefined;
  enabled?: boolean | undefined;
  id: string;
  message_types?: Array<string> | undefined;
  model_config?: RiskPolicyModelConfig$Outbound | undefined;
  name: string;
  presidio_entities?: Array<string> | undefined;
  presidio_score_threshold?: number | undefined;
  prompt?: string | undefined;
  prompt_injection_rules?: Array<string> | undefined;
  scope_exempt?: string | undefined;
  scope_include?: string | undefined;
  sources?: Array<string> | undefined;
  user_message?: string | undefined;
};
/** @internal */
export declare const UpdateRiskPolicyRequestBody$outboundSchema: z.ZodMiniType<
  UpdateRiskPolicyRequestBody$Outbound,
  UpdateRiskPolicyRequestBody
>;
export declare function updateRiskPolicyRequestBodyToJSON(
  updateRiskPolicyRequestBody: UpdateRiskPolicyRequestBody,
): string;
//# sourceMappingURL=updateriskpolicyrequestbody.d.ts.map
