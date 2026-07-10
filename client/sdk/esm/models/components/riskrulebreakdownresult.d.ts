import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskRuleBreakdownEntry } from "./riskrulebreakdownentry.js";
export type RiskRuleBreakdownResult = {
    /**
     * Category the breakdown is scoped to.
     */
    category: string;
    /**
     * Inclusive start of the window used.
     */
    from: Date;
    /**
     * Rules in this category, ordered by finding count descending.
     */
    rules: Array<RiskRuleBreakdownEntry>;
    /**
     * Exclusive end of the window used.
     */
    to: Date;
    /**
     * Total findings across all rules in this category and window.
     */
    total: number;
};
/** @internal */
export declare const RiskRuleBreakdownResult$inboundSchema: z.ZodMiniType<RiskRuleBreakdownResult, unknown>;
export declare function riskRuleBreakdownResultFromJSON(jsonString: string): SafeParseResult<RiskRuleBreakdownResult, SDKValidationError>;
//# sourceMappingURL=riskrulebreakdownresult.d.ts.map