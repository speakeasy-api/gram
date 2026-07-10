import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { UserSessionConsent } from "./usersessionconsent.js";
/**
 * Result type for listing user_session_consents.
 */
export type ListUserSessionConsentsResult = {
    items: Array<UserSessionConsent>;
    /**
     * Cursor for the next page; empty when exhausted.
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const ListUserSessionConsentsResult$inboundSchema: z.ZodMiniType<ListUserSessionConsentsResult, unknown>;
export declare function listUserSessionConsentsResultFromJSON(jsonString: string): SafeParseResult<ListUserSessionConsentsResult, SDKValidationError>;
//# sourceMappingURL=listusersessionconsentsresult.d.ts.map