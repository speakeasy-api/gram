import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * An issued user_session record. refresh_token_hash is never returned.
 */
export type UserSession = {
    /**
     * Name of the MCP client that established the session, if known.
     */
    clientName?: string | undefined;
    createdAt: Date;
    /**
     * Terminal session expiry; ceiling on refresh_expires_at.
     */
    expiresAt: Date;
    /**
     * The user_session id.
     */
    id: string;
    /**
     * Slug of the user_session_issuer that gated this session.
     */
    issuerSlug: string;
    /**
     * Current access-token JTI; used by the revocation path.
     */
    jti: string;
    /**
     * Next refresh deadline.
     */
    refreshExpiresAt: Date;
    /**
     * When the session was revoked, if it has been.
     */
    revokedAt?: Date | undefined;
    /**
     * Resolved human-readable name of the subject, if known.
     */
    subjectDisplayName?: string | undefined;
    /**
     * Subject kind: 'user', 'apikey', or 'anonymous'.
     */
    subjectType: string;
    /**
     * The session's subject URN (user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>).
     */
    subjectUrn: string;
    updatedAt: Date;
    /**
     * The issuing user_session_issuer id.
     */
    userSessionIssuerId: string;
};
/** @internal */
export declare const UserSession$inboundSchema: z.ZodMiniType<UserSession, unknown>;
export declare function userSessionFromJSON(jsonString: string): SafeParseResult<UserSession, SDKValidationError>;
//# sourceMappingURL=usersession.d.ts.map