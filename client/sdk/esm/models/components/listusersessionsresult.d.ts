import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { UserSession } from "./usersession.js";
/**
 * Result type for listing user_sessions.
 */
export type ListUserSessionsResult = {
    items: Array<UserSession>;
    /**
     * Cursor for the next page; empty when exhausted.
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const ListUserSessionsResult$inboundSchema: z.ZodMiniType<ListUserSessionsResult, unknown>;
export declare function listUserSessionsResultFromJSON(jsonString: string): SafeParseResult<ListUserSessionsResult, SDKValidationError>;
//# sourceMappingURL=listusersessionsresult.d.ts.map