import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import { RemoteSessionIssuerDraft } from "../models/components/remotesessionissuerdraft.js";
import { CreateRemoteSessionIssuerRequest, CreateRemoteSessionIssuerSecurity } from "../models/operations/createremotesessionissuer.js";
import { DeleteRemoteSessionIssuerRequest, DeleteRemoteSessionIssuerSecurity } from "../models/operations/deleteremotesessionissuer.js";
import { DiscoverRemoteSessionIssuerRequest, DiscoverRemoteSessionIssuerSecurity } from "../models/operations/discoverremotesessionissuer.js";
import { GetRemoteSessionIssuerRequest, GetRemoteSessionIssuerSecurity } from "../models/operations/getremotesessionissuer.js";
import { ListRemoteSessionIssuersRequest, ListRemoteSessionIssuersResponse, ListRemoteSessionIssuersSecurity } from "../models/operations/listremotesessionissuers.js";
import { UpdateRemoteSessionIssuerRequest, UpdateRemoteSessionIssuerSecurity } from "../models/operations/updateremotesessionissuer.js";
import { PageIterator } from "../types/operations.js";
export declare class RemoteSessionIssuers extends ClientSDK {
    /**
     * createRemoteSessionIssuer remoteSessionIssuers
     *
     * @remarks
     * Create a new remote_session_issuer.
     */
    create(request: CreateRemoteSessionIssuerRequest, security?: CreateRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionIssuer>;
    /**
     * deleteRemoteSessionIssuer remoteSessionIssuers
     *
     * @remarks
     * Soft-delete a remote_session_issuer. Blocked if any remote_session_clients still reference it.
     */
    delete(request: DeleteRemoteSessionIssuerRequest, security?: DeleteRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * discoverRemoteSessionIssuer remoteSessionIssuers
     *
     * @remarks
     * Hit an upstream issuer's RFC 8414 .well-known/oauth-authorization-server document and return a draft suitable for createRemoteSessionIssuer. No persistence.
     */
    discover(request: DiscoverRemoteSessionIssuerRequest, security?: DiscoverRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionIssuerDraft>;
    /**
     * getRemoteSessionIssuer remoteSessionIssuers
     *
     * @remarks
     * Get a remote_session_issuer by id or by slug. Provide exactly one.
     */
    get(request?: GetRemoteSessionIssuerRequest | undefined, security?: GetRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionIssuer>;
    /**
     * listRemoteSessionIssuers remoteSessionIssuers
     *
     * @remarks
     * List remote_session_issuers in the caller's project.
     */
    list(request?: ListRemoteSessionIssuersRequest | undefined, security?: ListRemoteSessionIssuersSecurity | undefined, options?: RequestOptions): Promise<PageIterator<ListRemoteSessionIssuersResponse, {
        cursor: string;
    }>>;
    /**
     * updateRemoteSessionIssuer remoteSessionIssuers
     *
     * @remarks
     * Update fields on an existing remote_session_issuer.
     */
    update(request: UpdateRemoteSessionIssuerRequest, security?: UpdateRemoteSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionIssuer>;
}
//# sourceMappingURL=remotesessionissuers.d.ts.map