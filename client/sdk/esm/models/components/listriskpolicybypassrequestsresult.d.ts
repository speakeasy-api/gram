import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RiskPolicyBypassRequest } from "./riskpolicybypassrequest.js";
export type ListRiskPolicyBypassRequestsResult = {
    /**
     * Current risk policy bypass request records.
     */
    requests: Array<RiskPolicyBypassRequest>;
};
/** @internal */
export declare const ListRiskPolicyBypassRequestsResult$inboundSchema: z.ZodMiniType<ListRiskPolicyBypassRequestsResult, unknown>;
export declare function listRiskPolicyBypassRequestsResultFromJSON(jsonString: string): SafeParseResult<ListRiskPolicyBypassRequestsResult, SDKValidationError>;
//# sourceMappingURL=listriskpolicybypassrequestsresult.d.ts.map