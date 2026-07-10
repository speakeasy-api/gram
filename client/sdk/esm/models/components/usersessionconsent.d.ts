import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A user_session_consent record. Per-client (not per-issuer) consent.
 */
export type UserSessionConsent = {
    consentedAt: Date;
    createdAt: Date;
    /**
     * The user_session_consent id.
     */
    id: string;
    /**
     * SHA-256 of the sorted list of remote_session_issuer ids on the client's owning issuer at consent time.
     */
    remoteSetHash: string;
    /**
     * The consenting subject URN (user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>).
     */
    subjectUrn: string;
    updatedAt: Date;
    /**
     * The user_session_client this consent binds to.
     */
    userSessionClientId: string;
};
/** @internal */
export declare const UserSessionConsent$inboundSchema: z.ZodMiniType<UserSessionConsent, unknown>;
export declare function userSessionConsentFromJSON(jsonString: string): SafeParseResult<UserSessionConsent, SDKValidationError>;
//# sourceMappingURL=usersessionconsent.d.ts.map