import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { RiskPolicyModelConfig, RiskPolicyModelConfig$Outbound } from "./riskpolicymodelconfig.js";
/**
 * Policy action: flag or block.
 */
export declare const Action: {
    readonly Flag: "flag";
    readonly Block: "block";
};
/**
 * Policy action: flag or block.
 */
export type Action = ClosedEnum<typeof Action>;
/**
 * Policy audience type: everyone or targeted.
 */
export declare const AudienceType: {
    readonly Everyone: "everyone";
    readonly Targeted: "targeted";
};
/**
 * Policy audience type: everyone or targeted.
 */
export type AudienceType = ClosedEnum<typeof AudienceType>;
/**
 * Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge). Defaults to standard.
 */
export declare const PolicyType: {
    readonly Standard: "standard";
    readonly PromptBased: "prompt_based";
};
/**
 * Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge). Defaults to standard.
 */
export type PolicyType = ClosedEnum<typeof PolicyType>;
export type CreateRiskPolicyRequestBody = {
    /**
     * Policy action: flag or block.
     */
    action?: Action | undefined;
    /**
     * For the account_identity source: corporate email domains considered approved. Sessions whose AI-account email domain is not listed are flagged. Empty/omitted leaves the domain rule inert.
     */
    approvedEmailDomains?: Array<string> | undefined;
    /**
     * Principal URNs this policy applies to. For audience_type=everyone, the server stores user:all.
     */
    audiencePrincipalUrns?: Array<string> | undefined;
    /**
     * Policy audience type: everyone or targeted.
     */
    audienceType?: AudienceType | undefined;
    /**
     * Whether the policy name should be auto-generated.
     */
    autoName?: boolean | undefined;
    /**
     * Custom detection rule ids to attach as detectors: a match produces a finding.
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
     * Message types this policy applies to. When empty or omitted, the policy scans all supported types.
     */
    messageTypes?: Array<string> | undefined;
    modelConfig?: RiskPolicyModelConfig | undefined;
    /**
     * The policy name. If omitted, a name will be auto-generated.
     */
    name?: string | undefined;
    /**
     * Policy type: standard (regex/presidio/custom detection) or prompt_based (LLM-judge). Defaults to standard.
     */
    policyType?: PolicyType | undefined;
    /**
     * Presidio entity types to detect.
     */
    presidioEntities?: Array<string> | undefined;
    /**
     * Minimum Presidio confidence (0.0-1.0) a PII match must clear to surface. Omit/null applies the default (0.5).
     */
    presidioScoreThreshold?: number | undefined;
    /**
     * For prompt_based policies: the guardrail prompt the LLM judge evaluates each in-scope message against. Required when policy_type is prompt_based.
     */
    prompt?: string | undefined;
    /**
     * Prompt-injection detection rule ids to enable in addition to the heuristic baseline.
     */
    promptInjectionRules?: Array<string> | undefined;
    /**
     * CEL exemption predicate: the policy is skipped for a message when this boolean expression is true. Omit/empty means no inline exemption.
     */
    scopeExempt?: string | undefined;
    /**
     * CEL scope predicate: the policy evaluates a message only when this boolean expression is true (in addition to message_types). Omit/empty means all messages are in scope.
     */
    scopeInclude?: string | undefined;
    /**
     * Detection sources to enable.
     */
    sources?: Array<string> | undefined;
    /**
     * Optional message shown to end users when this policy blocks an action or surfaces a flagged finding.
     */
    userMessage?: string | undefined;
};
/** @internal */
export declare const Action$outboundSchema: z.ZodMiniEnum<typeof Action>;
/** @internal */
export declare const AudienceType$outboundSchema: z.ZodMiniEnum<typeof AudienceType>;
/** @internal */
export declare const PolicyType$outboundSchema: z.ZodMiniEnum<typeof PolicyType>;
/** @internal */
export type CreateRiskPolicyRequestBody$Outbound = {
    action: string;
    approved_email_domains?: Array<string> | undefined;
    audience_principal_urns?: Array<string> | undefined;
    audience_type: string;
    auto_name?: boolean | undefined;
    custom_rule_ids?: Array<string> | undefined;
    disabled_rules?: Array<string> | undefined;
    enabled?: boolean | undefined;
    message_types?: Array<string> | undefined;
    model_config?: RiskPolicyModelConfig$Outbound | undefined;
    name?: string | undefined;
    policy_type: string;
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
export declare const CreateRiskPolicyRequestBody$outboundSchema: z.ZodMiniType<CreateRiskPolicyRequestBody$Outbound, CreateRiskPolicyRequestBody>;
export declare function createRiskPolicyRequestBodyToJSON(createRiskPolicyRequestBody: CreateRiskPolicyRequestBody): string;
//# sourceMappingURL=createriskpolicyrequestbody.d.ts.map