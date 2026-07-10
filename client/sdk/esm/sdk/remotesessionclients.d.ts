import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { AttachUserSessionIssuerRequest, AttachUserSessionIssuerSecurity } from "../models/operations/attachusersessionissuer.js";
import { CloneClientFromOAuthProxyProviderRequest, CloneClientFromOAuthProxyProviderSecurity } from "../models/operations/cloneclientfromoauthproxyprovider.js";
import { CreateCimdRemoteSessionClientRequest, CreateCimdRemoteSessionClientSecurity } from "../models/operations/createcimdremotesessionclient.js";
import { CreateRemoteSessionClientRequest, CreateRemoteSessionClientSecurity } from "../models/operations/createremotesessionclient.js";
import { DeleteRemoteSessionClientRequest, DeleteRemoteSessionClientSecurity } from "../models/operations/deleteremotesessionclient.js";
import { DetachUserSessionIssuerRequest, DetachUserSessionIssuerSecurity } from "../models/operations/detachusersessionissuer.js";
import { GetRemoteSessionClientRequest, GetRemoteSessionClientSecurity } from "../models/operations/getremotesessionclient.js";
import { ListRemoteSessionClientsRequest, ListRemoteSessionClientsResponse, ListRemoteSessionClientsSecurity } from "../models/operations/listremotesessionclients.js";
import { UpdateRemoteSessionClientRequest, UpdateRemoteSessionClientSecurity } from "../models/operations/updateremotesessionclient.js";
import { PageIterator } from "../types/operations.js";
export declare class RemoteSessionClients extends ClientSDK {
    /**
     * attachUserSessionIssuer remoteSessionClients
     *
     * @remarks
     * Attach a user_session_issuer to a remote_session_client by recording the binding in the join table. Rejected when another client is already bound to the same user_session_issuer for this client's remote_session_issuer.
     */
    attachUserSessionIssuer(request: AttachUserSessionIssuerRequest, security?: AttachUserSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionClient>;
    /**
     * cloneClientFromOAuthProxyProvider remoteSessionClients
     *
     * @remarks
     * Platform-admin-only. Clone the client_id / client_secret from an existing oauth_proxy_provider into a new remote_session_client paired with the supplied issuers. The upstream secret stays server-side: it is read from the proxy provider's stored secrets, re-encrypted, and persisted on the remote_session_client row without ever crossing the wire.
     */
    cloneClientFromOAuthProxyProvider(request: CloneClientFromOAuthProxyProviderRequest, security?: CloneClientFromOAuthProxyProviderSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionClient>;
    /**
     * createRemoteSessionClient remoteSessionClients
     *
     * @remarks
     * Register a remote_session_client by supplying a client_id and optional client_secret obtained out-of-band from the upstream issuer.
     */
    create(request: CreateRemoteSessionClientRequest, security?: CreateRemoteSessionClientSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionClient>;
    /**
     * createCimd remoteSessionClients
     *
     * @remarks
     * Register a remote_session_client in Client ID Metadata Document (CIMD) mode. Gram generates the client_id (the URL of a hosted client metadata document) and serves the document publicly; the client carries no secret and authenticates with token_endpoint_auth_method=none. The owning issuer must advertise client_id_metadata_document_supported.
     */
    createCimd(request: CreateCimdRemoteSessionClientRequest, security?: CreateCimdRemoteSessionClientSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionClient>;
    /**
     * deleteRemoteSessionClient remoteSessionClients
     *
     * @remarks
     * Soft-delete a remote_session_client. Cascades to remote_sessions rows pointing at this client; affected principals are forced to re-authenticate.
     */
    delete(request: DeleteRemoteSessionClientRequest, security?: DeleteRemoteSessionClientSecurity | undefined, options?: RequestOptions): Promise<void>;
    /**
     * detachUserSessionIssuer remoteSessionClients
     *
     * @remarks
     * Detach a user_session_issuer from a remote_session_client by removing the binding from the join table. A no-op when the binding does not exist.
     */
    detachUserSessionIssuer(request: DetachUserSessionIssuerRequest, security?: DetachUserSessionIssuerSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionClient>;
    /**
     * getRemoteSessionClient remoteSessionClients
     *
     * @remarks
     * Get a remote_session_client by id.
     */
    get(request: GetRemoteSessionClientRequest, security?: GetRemoteSessionClientSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionClient>;
    /**
     * listRemoteSessionClients remoteSessionClients
     *
     * @remarks
     * List remote_session_clients in the caller's project.
     */
    list(request?: ListRemoteSessionClientsRequest | undefined, security?: ListRemoteSessionClientsSecurity | undefined, options?: RequestOptions): Promise<PageIterator<ListRemoteSessionClientsResponse, {
        cursor: string;
    }>>;
    /**
     * updateRemoteSessionClient remoteSessionClients
     *
     * @remarks
     * Rotate the client_secret or change the non-issuer settings on an existing remote_session_client. Issuer attachments are managed via attachUserSessionIssuer / detachUserSessionIssuer.
     */
    update(request: UpdateRemoteSessionClientRequest, security?: UpdateRemoteSessionClientSecurity | undefined, options?: RequestOptions): Promise<RemoteSessionClient>;
}
//# sourceMappingURL=remotesessionclients.d.ts.map