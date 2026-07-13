import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Severity level for findings produced by this rule.
 */
export declare const RiskCustomDetectionRuleSeverity: {
  readonly Info: "info";
  readonly Low: "low";
  readonly Medium: "medium";
  readonly High: "high";
  readonly Critical: "critical";
};
/**
 * Severity level for findings produced by this rule.
 */
export type RiskCustomDetectionRuleSeverity = ClosedEnum<
  typeof RiskCustomDetectionRuleSeverity
>;
export type RiskCustomDetectionRule = {
  /**
   * When the custom detection rule was created.
   */
  createdAt: Date;
  /**
   * Description of what the rule detects.
   */
  description: string;
  /**
   * CEL detection predicate: a boolean expression over message fields whose true verdict produces a finding. Supersedes regex.
   */
  detectionExpr?: string | undefined;
  /**
   * The custom detection rule ID.
   */
  id: string;
  /**
   * Legacy RE2-compatible regex pattern (read-only). Live for existing rules; evaluated as content.match(regex) when detection_expr is empty. New rules author detection_expr instead.
   */
  regex: string;
  /**
   * Stable rule identifier, prefixed with `custom.`.
   */
  ruleId: string;
  /**
   * Severity level for findings produced by this rule.
   */
  severity: RiskCustomDetectionRuleSeverity;
  /**
   * Human-readable title for the rule.
   */
  title: string;
  /**
   * When the custom detection rule was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const RiskCustomDetectionRuleSeverity$inboundSchema: z.ZodMiniEnum<
  typeof RiskCustomDetectionRuleSeverity
>;
/** @internal */
export declare const RiskCustomDetectionRule$inboundSchema: z.ZodMiniType<
  RiskCustomDetectionRule,
  unknown
>;
export declare function riskCustomDetectionRuleFromJSON(
  jsonString: string,
): SafeParseResult<RiskCustomDetectionRule, SDKValidationError>;
//# sourceMappingURL=riskcustomdetectionrule.d.ts.map
