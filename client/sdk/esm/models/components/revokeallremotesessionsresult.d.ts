import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Result type for revoking all of a client's remote_sessions.
 */
export type RevokeAllRemoteSessionsResult = {
    /**
     * Number of remote_sessions revoked.
     */
    revokedCount: number;
};
/** @internal */
export declare const RevokeAllRemoteSessionsResult$inboundSchema: z.ZodMiniType<RevokeAllRemoteSessionsResult, unknown>;
export declare function revokeAllRemoteSessionsResultFromJSON(jsonString: string): SafeParseResult<RevokeAllRemoteSessionsResult, SDKValidationError>;
//# sourceMappingURL=revokeallremotesessionsresult.d.ts.map