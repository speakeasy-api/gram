import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Severity level for findings produced by this rule.
 */
export declare const UpdateCustomDetectionRuleRequestBodySeverity: {
    readonly Info: "info";
    readonly Low: "low";
    readonly Medium: "medium";
    readonly High: "high";
    readonly Critical: "critical";
};
/**
 * Severity level for findings produced by this rule.
 */
export type UpdateCustomDetectionRuleRequestBodySeverity = ClosedEnum<typeof UpdateCustomDetectionRuleRequestBodySeverity>;
export type UpdateCustomDetectionRuleRequestBody = {
    /**
     * Description of what the rule detects.
     */
    description?: string | undefined;
    /**
     * CEL detection predicate: a boolean expression over message fields whose true verdict produces a finding.
     */
    detectionExpr?: string | undefined;
    /**
     * The custom detection rule ID.
     */
    id: string;
    /**
     * Deprecated legacy RE2 regex pattern; superseded by detection_expr. Accepted for backward compatibility.
     */
    regex?: string | undefined;
    /**
     * Severity level for findings produced by this rule.
     */
    severity: UpdateCustomDetectionRuleRequestBodySeverity;
    /**
     * Human-readable title for the rule.
     */
    title: string;
};
/** @internal */
export declare const UpdateCustomDetectionRuleRequestBodySeverity$outboundSchema: z.ZodMiniEnum<typeof UpdateCustomDetectionRuleRequestBodySeverity>;
/** @internal */
export type UpdateCustomDetectionRuleRequestBody$Outbound = {
    description?: string | undefined;
    detection_expr?: string | undefined;
    id: string;
    regex?: string | undefined;
    severity: string;
    title: string;
};
/** @internal */
export declare const UpdateCustomDetectionRuleRequestBody$outboundSchema: z.ZodMiniType<UpdateCustomDetectionRuleRequestBody$Outbound, UpdateCustomDetectionRuleRequestBody>;
export declare function updateCustomDetectionRuleRequestBodyToJSON(updateCustomDetectionRuleRequestBody: UpdateCustomDetectionRuleRequestBody): string;
//# sourceMappingURL=updatecustomdetectionrulerequestbody.d.ts.map