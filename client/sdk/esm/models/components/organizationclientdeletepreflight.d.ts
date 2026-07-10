import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Authoritative impact summary for deleting a remote_session_client: how many sessions it holds and the names of the MCP servers it is attached to.
 */
export type OrganizationClientDeletePreflight = {
    /**
     * Display names of MCP servers this client is attached to.
     */
    mcpServerNames: Array<string>;
    /**
     * Number of non-deleted remote_sessions minted against this client.
     */
    sessionCount: number;
};
/** @internal */
export declare const OrganizationClientDeletePreflight$inboundSchema: z.ZodMiniType<OrganizationClientDeletePreflight, unknown>;
export declare function organizationClientDeletePreflightFromJSON(jsonString: string): SafeParseResult<OrganizationClientDeletePreflight, SDKValidationError>;
//# sourceMappingURL=organizationclientdeletepreflight.d.ts.map