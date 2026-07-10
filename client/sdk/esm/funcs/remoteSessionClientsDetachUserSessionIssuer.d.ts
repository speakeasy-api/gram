import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DetachUserSessionIssuerRequest, DetachUserSessionIssuerSecurity } from "../models/operations/detachusersessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * detachUserSessionIssuer remoteSessionClients
 *
 * @remarks
 * Detach a user_session_issuer from a remote_session_client by removing the binding from the join table. A no-op when the binding does not exist.
 */
export declare function remoteSessionClientsDetachUserSessionIssuer(client: GramCore, request: DetachUserSessionIssuerRequest, security?: DetachUserSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionClient, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionClientsDetachUserSessionIssuer.d.ts.map