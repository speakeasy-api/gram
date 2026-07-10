import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskCustomDetectionRule } from "./riskcustomdetectionrule.js";
export type ListCustomDetectionRulesResult = {
    /**
     * The list of custom detection rules.
     */
    rules: Array<RiskCustomDetectionRule>;
};
/** @internal */
export declare const ListCustomDetectionRulesResult$inboundSchema: z.ZodMiniType<ListCustomDetectionRulesResult, unknown>;
export declare function listCustomDetectionRulesResultFromJSON(jsonString: string): SafeParseResult<ListCustomDetectionRulesResult, SDKValidationError>;
//# sourceMappingURL=listcustomdetectionrulesresult.d.ts.map