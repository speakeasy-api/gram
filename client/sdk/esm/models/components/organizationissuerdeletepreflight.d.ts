import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Authoritative impact summary for deleting a remote_session_issuer: how many clients reference it and the names of the MCP servers those clients are attached to.
 */
export type OrganizationIssuerDeletePreflight = {
    /**
     * Number of non-deleted remote_session_clients registered with this issuer.
     */
    clientCount: number;
    /**
     * Display names of MCP servers attached to this issuer's clients.
     */
    mcpServerNames: Array<string>;
};
/** @internal */
export declare const OrganizationIssuerDeletePreflight$inboundSchema: z.ZodMiniType<OrganizationIssuerDeletePreflight, unknown>;
export declare function organizationIssuerDeletePreflightFromJSON(jsonString: string): SafeParseResult<OrganizationIssuerDeletePreflight, SDKValidationError>;
//# sourceMappingURL=organizationissuerdeletepreflight.d.ts.map