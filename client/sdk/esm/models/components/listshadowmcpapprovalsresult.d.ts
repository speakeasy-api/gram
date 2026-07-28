import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ShadowMCPApproval } from "./shadowmcpapproval.js";
export type ListShadowMCPApprovalsResult = {
  /**
   * The approved shadow-MCP servers for the policy (URL- or command-keyed).
   */
  approvals: Array<ShadowMCPApproval>;
};
/** @internal */
export declare const ListShadowMCPApprovalsResult$inboundSchema: z.ZodMiniType<
  ListShadowMCPApprovalsResult,
  unknown
>;
export declare function listShadowMCPApprovalsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListShadowMCPApprovalsResult, SDKValidationError>;
//# sourceMappingURL=listshadowmcpapprovalsresult.d.ts.map
