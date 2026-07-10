import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteSessionClient } from "./remotesessionclient.js";
/**
 * An organization-administrator view of a remote_session_client: the client plus the number of MCP servers it is attached to and the number of active sessions minted against it.
 */
export type OrganizationRemoteSessionClient = {
    /**
     * Number of non-deleted (active) remote_sessions minted against this client.
     */
    activeSessionCount: number;
    /**
     * A remote_session_client record. client_secret_encrypted is never returned.
     */
    client: RemoteSessionClient;
    /**
     * Number of non-deleted MCP servers attached to this client (via user_session_issuers).
     */
    mcpServerCount: number;
};
/** @internal */
export declare const OrganizationRemoteSessionClient$inboundSchema: z.ZodMiniType<OrganizationRemoteSessionClient, unknown>;
export declare function organizationRemoteSessionClientFromJSON(jsonString: string): SafeParseResult<OrganizationRemoteSessionClient, SDKValidationError>;
//# sourceMappingURL=organizationremotesessionclient.d.ts.map