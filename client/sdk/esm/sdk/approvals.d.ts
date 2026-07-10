import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Approvals extends ClientSDK {
    /**
     * approveShadowMCP risk
     *
     * @remarks
     * Approve a shadow-MCP server so the named policy stops blocking calls to it. `match` is the same opaque server identifier surfaced in `RiskResult.match` — typically a server URL, stdio command, or `mcp__<server>__` prefix.
     */
    create(request: operations.ApproveShadowMCPRequest, security?: operations.ApproveShadowMCPSecurity | undefined, options?: RequestOptions): Promise<components.ShadowMCPApproval>;
    /**
     * revokeShadowMCPApproval risk
     *
     * @remarks
     * Remove a previously-approved shadow-MCP server for a policy.
     */
    delete(request: operations.RevokeShadowMCPApprovalRequest, security?: operations.RevokeShadowMCPApprovalSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * listShadowMCPApprovals risk
     *
     * @remarks
     * List shadow-MCP approvals (URL- or command-keyed) for a policy. Temporary Redis-backed storage; will move to a dedicated table once the feature graduates.
     */
    list(request: operations.ListShadowMCPApprovalsRequest, security?: operations.ListShadowMCPApprovalsSecurity | undefined, options?: RequestOptions): Promise<components.ListShadowMCPApprovalsResult>;
}
//# sourceMappingURL=approvals.d.ts.map