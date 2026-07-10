import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskPolicy } from "./riskpolicy.js";
export type ListRiskPoliciesResult = {
    /**
     * The list of risk policies.
     */
    policies: Array<RiskPolicy>;
};
/** @internal */
export declare const ListRiskPoliciesResult$inboundSchema: z.ZodMiniType<ListRiskPoliciesResult, unknown>;
export declare function listRiskPoliciesResultFromJSON(jsonString: string): SafeParseResult<ListRiskPoliciesResult, SDKValidationError>;
//# sourceMappingURL=listriskpoliciesresult.d.ts.map