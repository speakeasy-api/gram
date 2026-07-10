import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListUserSessionFacetsResult } from "../models/components/listusersessionfacetsresult.js";
import { MintUserSessionResponseBody } from "../models/components/mintusersessionresponsebody.js";
import { ListUserSessionFacetsRequest, ListUserSessionFacetsSecurity } from "../models/operations/listusersessionfacets.js";
import { ListUserSessionsRequest, ListUserSessionsResponse, ListUserSessionsSecurity } from "../models/operations/listusersessions.js";
import { MintUserSessionRequest, MintUserSessionSecurity } from "../models/operations/mintusersession.js";
import { RevokeUserSessionRequest, RevokeUserSessionSecurity } from "../models/operations/revokeusersession.js";
import { PageIterator } from "../types/operations.js";
export declare class UserSessions extends ClientSDK {
    /**
     * listUserSessions userSessions
     *
     * @remarks
     * List issued user_sessions in the caller's project. refresh_token_hash is never returned.
     */
    list(request?: ListUserSessionsRequest | undefined, security?: ListUserSessionsSecurity | undefined, options?: RequestOptions): Promise<PageIterator<ListUserSessionsResponse, {
        cursor: string;
    }>>;
    /**
     * listFacets userSessions
     *
     * @remarks
     * List available user session facet values (clients, users, servers) in the caller's project.
     */
    listFacets(request?: ListUserSessionFacetsRequest | undefined, security?: ListUserSessionFacetsSecurity | undefined, options?: RequestOptions): Promise<ListUserSessionFacetsResult>;
    /**
     * mintUserSession userSessions
     *
     * @remarks
     * Mint a user_session on behalf of the authenticated dashboard user, bound to an issuer-gated audience: either a toolset (/mcp) or a remote MCP server (/x/mcp). Exactly one of toolset_id or mcp_server_id must be provided. The minted JWT matches the shape /token would emit after a successful OAuth dance, so the runtime MCP gateway validates it through the same path as a real MCP client's bearer.
     */
    mint(request: MintUserSessionRequest, security?: MintUserSessionSecurity | undefined, options?: RequestOptions): Promise<MintUserSessionResponseBody>;
    /**
     * revokeUserSession userSessions
     *
     * @remarks
     * Push the session's jti into the revocation cache and soft-delete the row.
     */
    revoke(request: RevokeUserSessionRequest, security?: RevokeUserSessionSecurity | undefined, options?: RequestOptions): Promise<void>;
}
//# sourceMappingURL=usersessions.d.ts.map