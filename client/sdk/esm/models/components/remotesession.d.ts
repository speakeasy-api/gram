import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A remote_session record — Gram's upstream OAuth session for a (principal, remote_session_client) pair. access_token_encrypted and refresh_token_encrypted are never returned.
 */
export type RemoteSession = {
    /**
     * Upstream access-token expiry. Independent of refresh_expires_at.
     */
    accessExpiresAt: Date;
    createdAt: Date;
    /**
     * Whether the session holds an upstream refresh token. Gates the 'Refresh now' action; refresh_expires_at is insufficient because an upstream may issue a non-expiring refresh token. The token itself is never returned.
     */
    hasRefreshToken: boolean;
    /**
     * The remote_session id.
     */
    id: string;
    /**
     * Upstream refresh-token expiry. Null when the session has no refresh token.
     */
    refreshExpiresAt?: Date | undefined;
    /**
     * The remote_session_client this session was minted against.
     */
    remoteSessionClientId: string;
    /**
     * Scopes held by this session.
     */
    scopes: Array<string>;
    /**
     * Resolved display name when the subject is a Gram user. Absent for apikey/anonymous subjects or unresolved users.
     */
    subjectDisplayName?: string | undefined;
    /**
     * Resolved email when the subject is a Gram user. Absent for apikey/anonymous subjects or unresolved users.
     */
    subjectEmail?: string | undefined;
    /**
     * The session's subject URN (user:<id> | apikey:<uuid> | anonymous:<mcp-session-id>).
     */
    subjectUrn: string;
    updatedAt: Date;
    /**
     * The user_session_issuer this session is bound to.
     */
    userSessionIssuerId: string;
};
/** @internal */
export declare const RemoteSession$inboundSchema: z.ZodMiniType<RemoteSession, unknown>;
export declare function remoteSessionFromJSON(jsonString: string): SafeParseResult<RemoteSession, SDKValidationError>;
//# sourceMappingURL=remotesession.d.ts.map