import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskPolicyModelConfig } from "./riskpolicymodelconfig.js";
/**
 * Policy action: flag (log only) or block (deny in real-time).
 */
export declare const RiskPolicyAction: {
  readonly Flag: "flag";
  readonly Block: "block";
};
/**
 * Policy action: flag (log only) or block (deny in real-time).
 */
export type RiskPolicyAction = ClosedEnum<typeof RiskPolicyAction>;
/**
 * Policy audience type: everyone or targeted.
 */
export declare const RiskPolicyAudienceType: {
  readonly Everyone: "everyone";
  readonly Targeted: "targeted";
};
/**
 * Policy audience type: everyone or targeted.
 */
export type RiskPolicyAudienceType = ClosedEnum<typeof RiskPolicyAudienceType>;
/**
 * Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge).
 */
export declare const RiskPolicyPolicyType: {
  readonly Standard: "standard";
  readonly PromptBased: "prompt_based";
};
/**
 * Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge).
 */
export type RiskPolicyPolicyType = ClosedEnum<typeof RiskPolicyPolicyType>;
export type RiskPolicy = {
  /**
   * Policy action: flag (log only) or block (deny in real-time).
   */
  action: RiskPolicyAction;
  /**
   * For the account_identity source: corporate email domains considered approved. Sessions whose AI-account email domain is not listed are flagged. Empty means the domain rule is inert.
   */
  approvedEmailDomains?: Array<string> | undefined;
  /**
   * Principal URNs the policy applies to. Contains user:all when audience_type is everyone.
   */
  audiencePrincipalUrns: Array<string>;
  /**
   * Policy audience type: everyone or targeted.
   */
  audienceType: RiskPolicyAudienceType;
  /**
   * Whether the policy name is auto-generated. When true, the name is regenerated on each update.
   */
  autoName: boolean;
  /**
   * When the policy was created.
   */
  createdAt: Date;
  /**
   * Custom detection rule ids attached as detectors: a match produces a finding. Custom rules are pure detectors.
   */
  customRuleIds?: Array<string> | undefined;
  /**
   * Canonical rule_ids (e.g. 'secret.aws_access_token', 'pii.credit_card') the policy author has unchecked within an otherwise-enabled category. Empty means every rule in the selected categories runs; matching findings are dropped at scan time.
   */
  disabledRules?: Array<string> | undefined;
  /**
   * Whether the policy is active.
   */
  enabled: boolean;
  /**
   * The risk policy ID.
   */
  id: string;
  /**
   * Message types this policy applies to. When empty or omitted, applies to all types. Valid values: user_message, tool_request, tool_response, assistant_message.
   */
  messageTypes?: Array<string> | undefined;
  modelConfig?: RiskPolicyModelConfig | undefined;
  /**
   * The policy name.
   */
  name: string;
  /**
   * Number of messages not yet analyzed at the current policy version.
   */
  pendingMessages: number;
  /**
   * Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge).
   */
  policyType: RiskPolicyPolicyType;
  /**
   * Presidio entity types to scan for. When empty, scans all entities.
   */
  presidioEntities?: Array<string> | undefined;
  /**
   * Minimum Presidio confidence (0.0-1.0) a PII match must clear to surface. Omit/null applies the default (0.5).
   */
  presidioScoreThreshold?: number | undefined;
  /**
   * The project ID.
   */
  projectId: string;
  /**
   * For prompt_based policies: the guardrail prompt the LLM judge evaluates each in-scope message against. Null for standard policies.
   */
  prompt?: string | undefined;
  /**
   * Prompt-injection detection rule ids enabled in addition to the heuristic baseline. When empty, only heuristics run.
   */
  promptInjectionRules?: Array<string> | undefined;
  /**
   * CEL exemption predicate: the policy is skipped for a message when this boolean expression is true. Null/empty means no inline exemption.
   */
  scopeExempt?: string | undefined;
  /**
   * CEL scope predicate: the policy evaluates a message only when this boolean expression is true (in addition to message_types). Null/empty means all messages are in scope.
   */
  scopeInclude?: string | undefined;
  /**
   * Detection sources enabled for this policy.
   */
  sources: Array<string>;
  /**
   * Total number of messages in the project.
   */
  totalMessages: number;
  /**
   * When the policy was last updated.
   */
  updatedAt: Date;
  /**
   * Optional message shown to the end user when this policy blocks an action or surfaces a flagged finding. When unset, a default message is rendered.
   */
  userMessage?: string | undefined;
  /**
   * Policy version, incremented on each update.
   */
  version: number;
};
/** @internal */
export declare const RiskPolicyAction$inboundSchema: z.ZodMiniEnum<
  typeof RiskPolicyAction
>;
/** @internal */
export declare const RiskPolicyAudienceType$inboundSchema: z.ZodMiniEnum<
  typeof RiskPolicyAudienceType
>;
/** @internal */
export declare const RiskPolicyPolicyType$inboundSchema: z.ZodMiniEnum<
  typeof RiskPolicyPolicyType
>;
/** @internal */
export declare const RiskPolicy$inboundSchema: z.ZodMiniType<
  RiskPolicy,
  unknown
>;
export declare function riskPolicyFromJSON(
  jsonString: string,
): SafeParseResult<RiskPolicy, SDKValidationError>;
//# sourceMappingURL=riskpolicy.d.ts.map
