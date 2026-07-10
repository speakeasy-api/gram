import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ShadowMCPApprovalRequest } from "./shadowmcpapprovalrequest.js";
export type ListShadowMCPApprovalRequestsResult = {
    /**
     * Cursor for the next page of results.
     */
    nextCursor?: string | undefined;
    requests: Array<ShadowMCPApprovalRequest>;
};
/** @internal */
export declare const ListShadowMCPApprovalRequestsResult$inboundSchema: z.ZodMiniType<ListShadowMCPApprovalRequestsResult, unknown>;
export declare function listShadowMCPApprovalRequestsResultFromJSON(jsonString: string): SafeParseResult<ListShadowMCPApprovalRequestsResult, SDKValidationError>;
//# sourceMappingURL=listshadowmcpapprovalrequestsresult.d.ts.map