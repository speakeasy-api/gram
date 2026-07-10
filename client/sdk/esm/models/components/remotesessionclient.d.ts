import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * How the client authenticates at the issuer's token endpoint. Null resolves to client_secret_basic at runtime.
 */
export declare const RemoteSessionClientTokenEndpointAuthMethod: {
    readonly ClientSecretBasic: "client_secret_basic";
    readonly ClientSecretPost: "client_secret_post";
    readonly None: "none";
};
/**
 * How the client authenticates at the issuer's token endpoint. Null resolves to client_secret_basic at runtime.
 */
export type RemoteSessionClientTokenEndpointAuthMethod = ClosedEnum<typeof RemoteSessionClientTokenEndpointAuthMethod>;
/**
 * A remote_session_client record. client_secret_encrypted is never returned.
 */
export type RemoteSessionClient = {
    /**
     * Upstream OAuth audience sent on the authorize redirect and token exchange. Null omits the audience parameter.
     */
    audience?: string | undefined;
    /**
     * The client_id used to identify this client at the issuer's token and authorization endpoints.
     */
    clientId: string;
    clientIdIssuedAt: Date;
    /**
     * When set, the client is in Client ID Metadata Document (CIMD) mode: Gram hosts its OAuth client metadata document at this URL and uses it as the client_id. Null for non-CIMD clients.
     */
    clientIdMetadataUri?: string | undefined;
    /**
     * Null when the secret does not expire.
     */
    clientSecretExpiresAt?: Date | undefined;
    createdAt: Date;
    /**
     * The remote_session_client id.
     */
    id: string;
    /**
     * The owning organization id. Empty for legacy rows not yet backfilled and global clients.
     */
    organizationId: string;
    /**
     * The owning project id. Empty for organization-level and global clients.
     */
    projectId: string;
    /**
     * The owning remote_session_issuer id.
     */
    remoteSessionIssuerId: string;
    /**
     * Explicit upstream OAuth scopes the dance requests for this client. Null falls back to the issuer's scopes_supported.
     */
    scope?: Array<string> | undefined;
    /**
     * How the client authenticates at the issuer's token endpoint. Null resolves to client_secret_basic at runtime.
     */
    tokenEndpointAuthMethod?: RemoteSessionClientTokenEndpointAuthMethod | undefined;
    updatedAt: Date;
    /**
     * The user_session_issuers this client is attached to via the join table. Empty for a standalone client with no attachments.
     */
    userSessionIssuerIds: Array<string>;
};
/** @internal */
export declare const RemoteSessionClientTokenEndpointAuthMethod$inboundSchema: z.ZodMiniEnum<typeof RemoteSessionClientTokenEndpointAuthMethod>;
/** @internal */
export declare const RemoteSessionClient$inboundSchema: z.ZodMiniType<RemoteSessionClient, unknown>;
export declare function remoteSessionClientFromJSON(jsonString: string): SafeParseResult<RemoteSessionClient, SDKValidationError>;
//# sourceMappingURL=remotesessionclient.d.ts.map