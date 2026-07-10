import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListRemoteSessionsRequest, ListRemoteSessionsResponse, ListRemoteSessionsSecurity } from "../models/operations/listremotesessions.js";
import { RevokeRemoteSessionRequest, RevokeRemoteSessionSecurity } from "../models/operations/revokeremotesession.js";
import { PageIterator } from "../types/operations.js";
export declare class RemoteSessions extends ClientSDK {
    /**
     * listRemoteSessions remoteSessions
     *
     * @remarks
     * List remote_sessions in the caller's project. access_token_encrypted and refresh_token_encrypted are never returned — only metadata (access_expires_at, refresh_expires_at, scopes).
     */
    list(request?: ListRemoteSessionsRequest | undefined, security?: ListRemoteSessionsSecurity | undefined, options?: RequestOptions): Promise<PageIterator<ListRemoteSessionsResponse, {
        cursor: string;
    }>>;
    /**
     * revokeRemoteSession remoteSessions
     *
     * @remarks
     * Drop a remote_session row. The next /mcp call by that principal triggers a fresh authn challenge.
     */
    revoke(request: RevokeRemoteSessionRequest, security?: RevokeRemoteSessionSecurity | undefined, options?: RequestOptions): Promise<void>;
}
//# sourceMappingURL=remotesessions.d.ts.map