import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ShadowMCPApproval = {
  /**
   * When the approval was recorded.
   */
  approvedAt: Date;
  /**
   * User that recorded the approval.
   */
  approvedBy?: string | undefined;
  /**
   * The MCP server identifier this approval covers — typically a server URL, stdio command, or `mcp__<server>__` prefix (the same value surfaced in `RiskResult.match`).
   */
  match: string;
  /**
   * The risk policy ID this approval is scoped to.
   */
  policyId: string;
  /**
   * Display name of the MCP server, when known.
   */
  serverName?: string | undefined;
};
/** @internal */
export declare const ShadowMCPApproval$inboundSchema: z.ZodMiniType<
  ShadowMCPApproval,
  unknown
>;
export declare function shadowMCPApprovalFromJSON(
  jsonString: string,
): SafeParseResult<ShadowMCPApproval, SDKValidationError>;
//# sourceMappingURL=shadowmcpapproval.d.ts.map
