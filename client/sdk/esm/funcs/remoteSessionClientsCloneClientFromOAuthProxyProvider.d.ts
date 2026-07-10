import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CloneClientFromOAuthProxyProviderRequest, CloneClientFromOAuthProxyProviderSecurity } from "../models/operations/cloneclientfromoauthproxyprovider.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * cloneClientFromOAuthProxyProvider remoteSessionClients
 *
 * @remarks
 * Platform-admin-only. Clone the client_id / client_secret from an existing oauth_proxy_provider into a new remote_session_client paired with the supplied issuers. The upstream secret stays server-side: it is read from the proxy provider's stored secrets, re-encrypted, and persisted on the remote_session_client row without ever crossing the wire.
 */
export declare function remoteSessionClientsCloneClientFromOAuthProxyProvider(client: GramCore, request: CloneClientFromOAuthProxyProviderRequest, security?: CloneClientFromOAuthProxyProviderSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionClient, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionClientsCloneClientFromOAuthProxyProvider.d.ts.map