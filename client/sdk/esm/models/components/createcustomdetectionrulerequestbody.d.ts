import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Severity level for findings produced by this rule.
 */
export declare const Severity: {
  readonly Info: "info";
  readonly Low: "low";
  readonly Medium: "medium";
  readonly High: "high";
  readonly Critical: "critical";
};
/**
 * Severity level for findings produced by this rule.
 */
export type Severity = ClosedEnum<typeof Severity>;
export type CreateCustomDetectionRuleRequestBody = {
  /**
   * Description of what the rule detects.
   */
  description?: string | undefined;
  /**
   * CEL detection predicate: a boolean expression over message fields whose true verdict produces a finding.
   */
  detectionExpr?: string | undefined;
  /**
   * Deprecated legacy RE2 regex pattern; superseded by detection_expr. Accepted for backward compatibility.
   */
  regex?: string | undefined;
  /**
   * Stable rule identifier, prefixed with `custom.`.
   */
  ruleId: string;
  /**
   * Severity level for findings produced by this rule.
   */
  severity?: Severity | undefined;
  /**
   * Human-readable title for the rule.
   */
  title: string;
};
/** @internal */
export declare const Severity$outboundSchema: z.ZodMiniEnum<typeof Severity>;
/** @internal */
export type CreateCustomDetectionRuleRequestBody$Outbound = {
  description?: string | undefined;
  detection_expr?: string | undefined;
  regex?: string | undefined;
  rule_id: string;
  severity: string;
  title: string;
};
/** @internal */
export declare const CreateCustomDetectionRuleRequestBody$outboundSchema: z.ZodMiniType<
  CreateCustomDetectionRuleRequestBody$Outbound,
  CreateCustomDetectionRuleRequestBody
>;
export declare function createCustomDetectionRuleRequestBodyToJSON(
  createCustomDetectionRuleRequestBody: CreateCustomDetectionRuleRequestBody,
): string;
//# sourceMappingURL=createcustomdetectionrulerequestbody.d.ts.map
