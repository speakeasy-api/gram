import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ShadowMCPAccessRule } from "./shadowmcpaccessrule.js";
import { ShadowMCPApprovalRequest } from "./shadowmcpapprovalrequest.js";
export type ShadowMCPApprovalDecisionResult = {
    request: ShadowMCPApprovalRequest;
    rule?: ShadowMCPAccessRule | undefined;
    rules: Array<ShadowMCPAccessRule>;
};
/** @internal */
export declare const ShadowMCPApprovalDecisionResult$inboundSchema: z.ZodMiniType<ShadowMCPApprovalDecisionResult, unknown>;
export declare function shadowMCPApprovalDecisionResultFromJSON(jsonString: string): SafeParseResult<ShadowMCPApprovalDecisionResult, SDKValidationError>;
//# sourceMappingURL=shadowmcpapprovaldecisionresult.d.ts.map