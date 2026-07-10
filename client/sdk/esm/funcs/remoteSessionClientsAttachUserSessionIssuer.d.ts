import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionClient } from "../models/components/remotesessionclient.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AttachUserSessionIssuerRequest, AttachUserSessionIssuerSecurity } from "../models/operations/attachusersessionissuer.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * attachUserSessionIssuer remoteSessionClients
 *
 * @remarks
 * Attach a user_session_issuer to a remote_session_client by recording the binding in the join table. Rejected when another client is already bound to the same user_session_issuer for this client's remote_session_issuer.
 */
export declare function remoteSessionClientsAttachUserSessionIssuer(client: GramCore, request: AttachUserSessionIssuerRequest, security?: AttachUserSessionIssuerSecurity | undefined, options?: RequestOptions): APIPromise<Result<RemoteSessionClient, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=remoteSessionClientsAttachUserSessionIssuer.d.ts.map