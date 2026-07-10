import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RemoteSession } from "./remotesession.js";
/**
 * Result type for listing remote_sessions.
 */
export type ListRemoteSessionsResult = {
    items: Array<RemoteSession>;
    /**
     * Cursor for the next page; empty when exhausted.
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const ListRemoteSessionsResult$inboundSchema: z.ZodMiniType<ListRemoteSessionsResult, unknown>;
export declare function listRemoteSessionsResultFromJSON(jsonString: string): SafeParseResult<ListRemoteSessionsResult, SDKValidationError>;
//# sourceMappingURL=listremotesessionsresult.d.ts.map