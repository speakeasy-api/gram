import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteSession } from "./remotesession.js";
/**
 * Result type for the remote_sessions minted against a remote_session_client.
 */
export type ListOrganizationRemoteSessionsResult = {
    items: Array<RemoteSession>;
    /**
     * Cursor for the next page; empty when exhausted.
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionsResult$inboundSchema: z.ZodMiniType<ListOrganizationRemoteSessionsResult, unknown>;
export declare function listOrganizationRemoteSessionsResultFromJSON(jsonString: string): SafeParseResult<ListOrganizationRemoteSessionsResult, SDKValidationError>;
//# sourceMappingURL=listorganizationremotesessionsresult.d.ts.map