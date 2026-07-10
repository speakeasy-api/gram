import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Suggested severity level.
 */
export declare const SuggestCustomDetectionRuleResultSeverity: {
    readonly Info: "info";
    readonly Low: "low";
    readonly Medium: "medium";
    readonly High: "high";
    readonly Critical: "critical";
};
/**
 * Suggested severity level.
 */
export type SuggestCustomDetectionRuleResultSeverity = ClosedEnum<typeof SuggestCustomDetectionRuleResultSeverity>;
export type SuggestCustomDetectionRuleResult = {
    /**
     * Description of what the rule detects and why it matters.
     */
    description: string;
    /**
     * Suggested CEL detection predicate.
     */
    detectionExpr?: string | undefined;
    /**
     * Deprecated legacy regex suggestion; superseded by detection_expr. Present for backward compatibility.
     */
    regex: string;
    /**
     * Suggested stable identifier, prefixed with `custom.`.
     */
    ruleId: string;
    /**
     * Suggested severity level.
     */
    severity: SuggestCustomDetectionRuleResultSeverity;
    /**
     * Short, human-friendly title for the rule.
     */
    title: string;
};
/** @internal */
export declare const SuggestCustomDetectionRuleResultSeverity$inboundSchema: z.ZodMiniEnum<typeof SuggestCustomDetectionRuleResultSeverity>;
/** @internal */
export declare const SuggestCustomDetectionRuleResult$inboundSchema: z.ZodMiniType<SuggestCustomDetectionRuleResult, unknown>;
export declare function suggestCustomDetectionRuleResultFromJSON(jsonString: string): SafeParseResult<SuggestCustomDetectionRuleResult, SDKValidationError>;
//# sourceMappingURL=suggestcustomdetectionruleresult.d.ts.map